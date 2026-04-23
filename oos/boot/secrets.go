package boot

import (
	"onisin.com/oos/helper"
	"onisin.com/oos/secrets"
)

func initSecretsFromJWT(idToken string) error {
	if helper.Meta.Vault.URL == "" {
		return nil
	}
	return secrets.InitWithJWT(helper.Meta.Vault.URL, idToken, "oos")
}
