package secrets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func exchangeJWTForVaultToken(vaultURL, idToken string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"role": "oos-role",
		"jwt":  idToken,
	})

	req, err := http.NewRequest("POST", vaultURL+"/v1/auth/jwt/login", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault erreichbar?: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vault antwort %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("vault antwort dekodieren: %w", err)
	}
	if result.Auth.ClientToken == "" {
		return "", fmt.Errorf("vault gab keinen token zurück")
	}

	return result.Auth.ClientToken, nil
}
