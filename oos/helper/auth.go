package helper

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"onisin.com/oos-common/dsl"
)

type AuthResult struct {
	AccessToken  string
	IDToken      string
	RefreshToken string
	Claims       map[string]interface{}
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func ApplyJWTClaims(claims map[string]interface{}) {
	log.Println("[JWT] ── Claims ─────────────────────────────────────────────")
	for k, v := range claims {
		log.Printf("[JWT]   %-40s = %v", k, v)
	}
	log.Println("[JWT] ───────────────────────────────────────────────────────")

	if rawGroups, ok := claims["groups"]; ok {
		if items, ok := rawGroups.([]interface{}); ok {
			ActiveGroups = nil
			for _, item := range items {
				if s, ok := item.(string); ok && s != "" {
					ActiveGroups = append(ActiveGroups, s)
				}
			}
			log.Printf("[AUTH] Gruppen: %v", ActiveGroups)
		}
	}

	if len(ActiveGroups) == 0 {
		const rolesKey = "urn:zitadel:iam:org:project:roles"
		if rawRoles, ok := claims[rolesKey]; ok {
			if rolesMap, ok := rawRoles.(map[string]interface{}); ok {
				ActiveGroups = nil
				for roleName := range rolesMap {
					if roleName != "" {
						ActiveGroups = append(ActiveGroups, roleName)
					}
				}
				log.Printf("[AUTH] Zitadel Rollen: %v", ActiveGroups)
			}
		}
	}

	if len(ActiveGroups) == 0 {
		usernameRaw, _ := claims["preferred_username"].(string)
		if usernameRaw == "" {
			emailRaw, _ := claims["email"].(string)
			if at := strings.Index(emailRaw, "@"); at > 0 {
				usernameRaw = emailRaw[:at]
			}
		}
		switch usernameRaw {
		case "admin":
			ActiveGroups = []string{"oos-admin"}
		case "user":
			ActiveGroups = []string{"oos-user"}
		}
		if len(ActiveGroups) > 0 {
			log.Printf("[AUTH] Gruppen via Username-Mapping: %v", ActiveGroups)
		} else {
			log.Printf("[AUTH] ⚠️  Keine Gruppen im JWT und kein Username-Mapping")
		}
	}

	email, _ := claims["email"].(string)
	username, _ := claims["preferred_username"].(string)
	if username == "" {
		if at := strings.Index(email, "@"); at > 0 {
			username = email[:at]
		} else {
			username = email
		}
	}
	SetActiveIdentity(email, username)
	log.Printf("[AUTH] Benutzer: %s (%s) Gruppen: %v", username, email, ActiveGroups)
}

// ActiveGroupForOOSP returns the single group that should be sent to oosp
// in the X-OOS-Group header.
//
// A user can belong to several groups. oosp today expects exactly one group
// per request so it can resolve a single role and permission set. We pick
// the one with the highest-priority role (admin > manager > user), matching
// ResolveRole's logic — same priority table, different return value.
//
// Returns "" when no group is active; the caller should then not set the
// header (or clear it) so oosp falls back to its own default.
func ActiveGroupForOOSP() string {
	if len(ActiveGroups) == 0 {
		return ""
	}
	priority := map[string]int{"admin": 3, "manager": 2, "user": 1}
	best := ""
	bestPrio := 0
	for _, group := range ActiveGroups {
		for _, part := range strings.Split(group, "-") {
			if p, ok := priority[part]; ok && p > bestPrio {
				best = group
				bestPrio = p
			}
		}
	}
	if best != "" {
		return best
	}
	// No prioritised match — fall back to the first group so we at least
	// send something known to the server instead of silently using admin.
	return ActiveGroups[0]
}

func ResolveRole(ast *dsl.OOSAst, groups []string) string {
	if ast == nil || len(groups) == 0 {
		return ""
	}
	priority := map[string]int{"admin": 3, "manager": 2, "user": 1}
	best := ""
	bestPrio := 0
	for _, group := range groups {
		for _, part := range strings.Split(group, "-") {
			if p, ok := priority[part]; ok && p > bestPrio {
				best = part
				bestPrio = p
			}
		}
	}
	return best
}

func GetVaultCert(idToken string) (certPEM, keyPEM string, err error) {

	loginBody, _ := json.Marshal(map[string]string{
		"role": "oos-role",
		"jwt":  idToken,
	})
	loginReq, _ := http.NewRequest("POST",
		Meta.Vault.URL+"/v1/auth/jwt/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")

	loginResp, err := httpClient.Do(loginReq)
	if err != nil {
		return "", "", fmt.Errorf("vault login: %w", err)
	}
	defer loginResp.Body.Close()

	body, _ := io.ReadAll(loginResp.Body)
	if loginResp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("vault login HTTP %d: %s", loginResp.StatusCode, body)
	}

	var loginResult struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	json.Unmarshal(body, &loginResult) //nolint:errcheck
	if loginResult.Auth.ClientToken == "" {
		return "", "", fmt.Errorf("vault gab keinen Token zurück")
	}

	pkiBody, _ := json.Marshal(map[string]interface{}{
		"common_name": "localhost",
		"alt_names":   "localhost",
		"ip_sans":     "127.0.0.1",
		"ttl":         "720h",
	})
	pkiReq, _ := http.NewRequest("POST",
		Meta.Vault.URL+"/v1/pki/issue/oos-role", bytes.NewReader(pkiBody))
	pkiReq.Header.Set("X-Vault-Token", loginResult.Auth.ClientToken)
	pkiReq.Header.Set("Content-Type", "application/json")

	pkiResp, err := httpClient.Do(pkiReq)
	if err != nil {
		return "", "", fmt.Errorf("vault pki: %w", err)
	}
	defer pkiResp.Body.Close()

	if pkiResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(pkiResp.Body)
		return "", "", fmt.Errorf("vault pki HTTP %d: %s", pkiResp.StatusCode, body)
	}

	var vr struct {
		Data struct {
			Cert string `json:"certificate"`
			Key  string `json:"private_key"`
		} `json:"data"`
	}
	json.NewDecoder(pkiResp.Body).Decode(&vr) //nolint:errcheck
	return vr.Data.Cert, vr.Data.Key, nil
}

func decodeJWTClaims(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("kein gültiger JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c map[string]interface{}
	json.Unmarshal(payload, &c) //nolint:errcheck
	return c, nil
}
