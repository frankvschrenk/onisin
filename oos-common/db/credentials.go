package db

// credentials.go — Benutzername und Passwort aus der konfigurierten Quelle holen.
//
// Vier Quellen werden unterstützt:
//
//	vault  — direkte Vault/OpenBao KV v2 HTTP API
//	file   — gemountete Datei (Vault Agent, CSI Driver, ESO, K8s Secret Volume)
//	env    — Umgebungsvariablen (Helm-Deployments ohne Vault)
//	inline — direkt im JSON (nur Demo / lokale Entwicklung)

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Credentials enthält aufgelöstes Benutzername und Passwort.
type Credentials struct {
	Username string
	Password string
}

// ResolveCredentials holt Benutzername und Passwort gemäß CredentialRef.
// vaultURL und vaultToken werden nur benötigt wenn source = "vault".
func ResolveCredentials(ref CredentialRef, vaultURL, vaultToken string) (Credentials, error) {
	switch ref.Source {
	case "vault":
		return resolveVault(ref, vaultURL, vaultToken)
	case "file":
		return resolveFile(ref)
	case "env":
		return resolveEnv(ref)
	case "inline":
		return resolveInline(ref)
	default:
		return Credentials{}, fmt.Errorf("unbekannte credential source %q — erlaubt: vault, file, env, inline", ref.Source)
	}
}

// ── vault ─────────────────────────────────────────────────────────────────────

var credHTTP = &http.Client{Timeout: 10 * time.Second}

// resolveVault liest Credentials aus Vault KV v2.
// Erwartet am angegebenen Pfad: {"username": "...", "password": "..."}
func resolveVault(ref CredentialRef, vaultURL, vaultToken string) (Credentials, error) {
	if vaultURL == "" {
		return Credentials{}, fmt.Errorf("vault source: vaultURL nicht konfiguriert")
	}
	if vaultToken == "" {
		return Credentials{}, fmt.Errorf("vault source: vaultToken nicht konfiguriert")
	}
	if ref.Path == "" {
		return Credentials{}, fmt.Errorf("vault source: path fehlt")
	}

	url := strings.TrimRight(vaultURL, "/") + "/v1/" + ref.Path
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Vault-Token", vaultToken)

	resp, err := credHTTP.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("vault get %q: %w", ref.Path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Credentials{}, fmt.Errorf("vault: kein Eintrag unter %q", ref.Path)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Credentials{}, fmt.Errorf("vault get %q: status %d: %s", ref.Path, resp.StatusCode, body)
	}

	var result struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Credentials{}, fmt.Errorf("vault antwort parsen: %w", err)
	}

	creds := Credentials{
		Username: result.Data.Data["username"],
		Password: result.Data.Data["password"],
	}
	if creds.Username == "" || creds.Password == "" {
		return Credentials{}, fmt.Errorf("vault: %q enthält kein username/password", ref.Path)
	}
	return creds, nil
}

// ── file ──────────────────────────────────────────────────────────────────────

// resolveFile liest Credentials aus gemounteten Secret-Dateien.
// Erwartet im Verzeichnis ref.Dir:
//   - username  (eine Zeile)
//   - password  (eine Zeile)
//
// Funktioniert identisch für:
//   - Vault Agent Injector  (/vault/secrets/<name>)
//   - Secrets Store CSI Driver
//   - External Secrets Operator
//   - K8s Secret als Volume
func resolveFile(ref CredentialRef) (Credentials, error) {
	if ref.Dir == "" {
		return Credentials{}, fmt.Errorf("file source: dir fehlt")
	}

	username, err := readSecretFile(filepath.Join(ref.Dir, "username"))
	if err != nil {
		return Credentials{}, fmt.Errorf("file source: %w", err)
	}

	password, err := readSecretFile(filepath.Join(ref.Dir, "password"))
	if err != nil {
		return Credentials{}, fmt.Errorf("file source: %w", err)
	}

	return Credentials{Username: username, Password: password}, nil
}

func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("datei lesen %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ── env ───────────────────────────────────────────────────────────────────────

// resolveEnv liest Credentials aus Umgebungsvariablen.
// Typisch für Helm-Deployments ohne Vault.
func resolveEnv(ref CredentialRef) (Credentials, error) {
	if ref.EnvUser == "" || ref.EnvPass == "" {
		return Credentials{}, fmt.Errorf("env source: env_user und env_pass müssen gesetzt sein")
	}

	username := os.Getenv(ref.EnvUser)
	if username == "" {
		return Credentials{}, fmt.Errorf("env source: %q nicht gesetzt oder leer", ref.EnvUser)
	}

	password := os.Getenv(ref.EnvPass)
	if password == "" {
		return Credentials{}, fmt.Errorf("env source: %q nicht gesetzt oder leer", ref.EnvPass)
	}

	return Credentials{Username: username, Password: password}, nil
}

// ── inline ────────────────────────────────────────────────────────────────────

// resolveInline gibt Credentials direkt aus der Konfiguration zurück.
// Nur für Demo und lokale Entwicklung — niemals in Produktion.
func resolveInline(ref CredentialRef) (Credentials, error) {
	if ref.Username == "" || ref.Password == "" {
		return Credentials{}, fmt.Errorf("inline source: username und password müssen im JSON stehen")
	}
	return Credentials{Username: ref.Username, Password: ref.Password}, nil
}
