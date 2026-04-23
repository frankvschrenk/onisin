package db

// datasource.go — Typen für eine Datenquelle in OOS.
//
// Eine DatasourceConfig beschreibt vollständig wie OOSP sich mit einer
// relationalen Datenbank verbindet: Typ, Host, Datenbankname, optionale
// Treiber-Parameter und — getrennt davon — woher Benutzername und Passwort
// kommen.
//
// Das JSON-Objekt wird als Value in etcd gespeichert:
//
//	/oos/services/oosp/dsn_<name>  →  { "type": "mysql", "host": "...", ... }
//
// Beispiele: siehe Datei-Ende.

import "encoding/json"

// DatasourceConfig beschreibt eine relationale Datenquelle vollständig.
type DatasourceConfig struct {
	// Type ist der Datenbanktyp.
	// Erlaubte Werte: "postgres", "oracle", "mysql"
	Type string `json:"type"`

	// Host ist Hostname und Port, z.B. "pg-svc:5432"
	Host string `json:"host"`

	// Database ist der Datenbankname bzw. Oracle Service Name.
	Database string `json:"database"`

	// Options enthält treiberspezifische Parameter.
	// Postgres:  {"sslmode": "require"}
	// MySQL:     {"charset": "utf8mb4", "tls": "skip-verify"}
	// Oracle:    {"sysdba": "false"}
	// Leer lassen wenn keine besonderen Optionen nötig sind.
	Options map[string]string `json:"options,omitempty"`

	// Credentials beschreibt woher Benutzername und Passwort kommen.
	Credentials CredentialRef `json:"credentials"`
}

// CredentialRef beschreibt die Herkunft von Benutzername und Passwort.
// Genau ein Source-Typ wird verwendet — die übrigen Felder werden ignoriert.
type CredentialRef struct {
	// Source bestimmt die Bezugsquelle.
	// Erlaubte Werte: "vault", "file", "env", "inline"
	Source string `json:"source"`

	// --- vault ---
	// Path ist der Vault KV v2 Pfad, z.B. "secret/data/oosp/crm"
	// Vault muss {"username": "...", "password": "..."} zurückgeben.
	Path string `json:"path,omitempty"`

	// --- file ---
	// Dir ist das Verzeichnis mit den Secret-Dateien.
	// Erwartet werden: <dir>/username  und  <dir>/password  (je eine Zeile)
	// Typische Werte: "/vault/secrets/crm"  (Vault Agent Injector)
	//                 "/var/run/secrets/oosp/crm"  (K8s Secret Volume / CSI)
	Dir string `json:"dir,omitempty"`

	// --- env ---
	// EnvUser ist der Name der Env-Var die den Benutzernamen enthält.
	// EnvPass ist der Name der Env-Var die das Passwort enthält.
	// Beispiel: "OOSP_CRM_USER", "OOSP_CRM_PASS"
	EnvUser string `json:"env_user,omitempty"`
	EnvPass string `json:"env_pass,omitempty"`

	// --- inline ---
	// Nur für lokale Entwicklung und Demo.
	// Niemals in Produktionsumgebungen verwenden.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// ParseDatasourceConfig parst einen etcd-Value (JSON) in eine DatasourceConfig.
func ParseDatasourceConfig(raw string) (*DatasourceConfig, error) {
	var cfg DatasourceConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

/*
Beispiele für etcd-Values:

─── Postgres mit Vault ───────────────────────────────────────────────────────
{
  "type":     "postgres",
  "host":     "pg-svc:5432",
  "database": "oos",
  "options":  { "sslmode": "require" },
  "credentials": {
    "source": "vault",
    "path":   "secret/data/oosp/oos"
  }
}

─── MySQL mit gemounteter Datei (Vault Agent / CSI / ESO / K8s Secret) ───────
{
  "type":     "mysql",
  "host":     "mysql-svc:3306",
  "database": "crm",
  "credentials": {
    "source": "file",
    "dir":    "/vault/secrets/crm"
  }
}

─── Oracle mit Env-Vars (Helm-Deployment) ────────────────────────────────────
{
  "type":     "oracle",
  "host":     "ora-svc:1521",
  "database": "ORCL",
  "credentials": {
    "source":   "env",
    "env_user": "OOSP_ERP_USER",
    "env_pass": "OOSP_ERP_PASS"
  }
}

─── Postgres inline (lokale Entwicklung / Demo) ──────────────────────────────
{
  "type":     "postgres",
  "host":     "localhost:5432",
  "database": "oos",
  "options":  { "sslmode": "disable" },
  "credentials": {
    "source":   "inline",
    "username": "oos",
    "password": "oos-dev-2026"
  }
}
*/
