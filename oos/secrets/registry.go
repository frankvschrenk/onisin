package secrets

import "fmt"

var Active Source

func Init(provider, url, token, project, environment, path string) error {
	switch provider {
	case "env", "":
		Active = NewEnvSource("OOS")
	case "vault":
		if url == "" || token == "" {
			return fmt.Errorf("secrets: vault benötigt url und token")
		}
		Active = NewVaultSource(url, token, "secret", path)
	default:
		return fmt.Errorf("secrets: unbekannter provider %q (erlaubt: env, vault)", provider)
	}
	return nil
}

func InitWithJWT(vaultURL, idToken, path string) error {
	if vaultURL == "" {
		return fmt.Errorf("secrets: vault url fehlt")
	}
	if idToken == "" {
		return fmt.Errorf("secrets: jwt fehlt")
	}

	vaultToken, err := exchangeJWTForVaultToken(vaultURL, idToken)
	if err != nil {
		devToken := "oos-dev-root-token"
		Active = NewVaultSource(vaultURL, devToken, "secret", path)
		return fmt.Errorf("secrets: vault jwt-auth: %w", err)
	}

	Active = NewVaultSource(vaultURL, vaultToken, "secret", path)
	return nil
}
