package pluginsrv

import (
	"log"
	"os"
	"strings"

	"onisin.com/oos-common/db"
)

func applyConfig() {
	dsn := getEnv("OOSP_DSN", "")

	cfg = oospConfig{
		Addr: getEnv("OOSP_SERVER_ADDR", ":9100"),
		DSN:  dsn,

		VectorBackend: getEnv("OOSP_VECTOR_BACKEND", "pg"),
		VectorAddr:    dsn,

		// LLM endpoint for embeddings — OpenAI-compatible API.
		LLMUrl:     getEnv("OOSP_LLM_URL", "http://localhost:11434"),
		LLMApiKey:  getEnv("OOSP_LLM_API_KEY", ""),
		EmbedModel: getEnv("OOSP_EMBED_MODEL", "granite-embedding:latest"),
	}

	if os.Getenv("OOSP_DEBUG") == "true" {
		DebugMode = true
	}

	cfg.Datasources = readDatasourcesFromEnv()

	log.Printf("[oosp] addr=%s debug=%v", cfg.Addr, DebugMode)

	if len(cfg.Datasources) > 0 {
		names := make([]string, 0, len(cfg.Datasources))
		for name := range cfg.Datasources {
			names = append(names, name)
		}
		log.Printf("[oosp] datasources: %s", strings.Join(names, ", "))
	}
}

func readDatasourcesFromEnv() map[string]db.DatasourceConfig {
	dsn := getEnv("OOSP_DSN", "")
	if dsn == "" {
		return map[string]db.DatasourceConfig{}
	}

	params := parseDSN(dsn)

	host := params["host"]
	if port := params["port"]; port != "" {
		host = host + ":" + port
	}

	return map[string]db.DatasourceConfig{
		"demo": {
			Type:     "postgres",
			Host:     host,
			Database: params["dbname"],
			Options:  map[string]string{"sslmode": params["sslmode"]},
			Credentials: db.CredentialRef{
				Source:   "inline",
				Username: params["user"],
				Password: params["password"],
			},
		},
	}
}

func parseDSN(dsn string) map[string]string {
	result := map[string]string{}
	for _, part := range strings.Fields(dsn) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			result[kv[0]] = kv[1]
		}
	}
	return result
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}


