package helper

import (
	"log"

	"onisin.com/oos-common/db"
	"onisin.com/oos-common/dsl"
	"onisin.com/oos-common/oosp"
	"onisin.com/oos-common/plugin"
	"onisin.com/oos/secrets"
)

var DsnRegistry map[string]any
var FailedDSNs []string

func init() {
	plugin.HTTPClientFactory = func(url string) (plugin.Caller, error) {
		return newOOSPClient(url)
	}
}

func InitDsnFromAST(ast *dsl.OOSAst) map[string]any {
	registry := make(map[string]any)
	FailedDSNs = nil

	for _, source := range ast.Sources {
		switch source.Type {
		case "postgres":
			client, err := db.ConnectPostgres(source.DSN)
			if err != nil {
				log.Printf("[DSN] ❌ '%s' (postgres): %v", source.Name, err)
				FailedDSNs = append(FailedDSNs, source.Name)
				continue
			}
			registry[source.Name] = client
			log.Printf("[DSN] ✅ '%s' (postgres)", source.Name)

		case "plugin":
			client, err := newOOSPClient(source.URL)
			if err != nil {
				log.Printf("[DSN] ⚠️  Plugin '%s' nicht erreichbar: %v", source.Name, err)
				FailedDSNs = append(FailedDSNs, source.Name)
				continue
			}
			registry[source.Name] = client
			log.Printf("[DSN] ✅ '%s' (plugin → %s)", source.Name, source.URL)

		default:
			log.Printf("[DSN] ⚠️  Unbekannter Typ '%s' für '%s'", source.Type, source.Name)
		}
	}

	if len(FailedDSNs) > 0 {
		log.Printf("[DSN] ⚠️  %d Quelle(n) nicht verfügbar: %v", len(FailedDSNs), FailedDSNs)
	}

	DsnRegistry = registry
	return registry
}

func newOOSPClient(oospURL string) (*oosp.Client, error) {
	if currentIDToken() != "" {
		return oosp.NewTLS(oospURL, currentIDToken)
	}
	log.Printf("[DSN] ⚠️  plain HTTP → oosp (kein Token)")
	return oosp.NewHTTP(oospURL)
}

func currentIDToken() string {
	if ActiveIDToken != "" {
		return ActiveIDToken
	}
	if secrets.Active == nil {
		return ""
	}
	raw, err := secrets.Active.Get("OOS_SESSION")
	if err != nil || raw == "" {
		return ""
	}
	session, err := DecodeSession(raw)
	if err != nil || session == nil {
		return ""
	}
	return session.IDToken
}
