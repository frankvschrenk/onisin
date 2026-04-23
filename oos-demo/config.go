package main

// config.go — Configuration loaded from demo.toml.
//
// oos-demo is the demo orchestrator for local development.
// In production, Kubernetes ConfigMaps and Secrets take over.
//
// LLM inference (Ollama) is NOT configured here — the user starts Ollama
// independently. oos reads its own LLM URL from oos.toml, oosp gets it
// via env vars that oos-demo forwards from the [llm] section below.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

const (
	oosDir       = ".oos"
	demoTOMLName = "demo.toml"
	distDirName  = "dist"
)

// Config holds the full oos-demo configuration from demo.toml.
type Config struct {
	PostgreSQL PostgreSQLConfig `toml:"postgresql"`
	Dex        DexConfig        `toml:"dex"`
	OOSP       ServiceConfig    `toml:"oosp"`
	LLM        LLMConfig        `toml:"llm"`
}

// LLMConfig holds the LLM endpoint configuration for oosp.
// Only URL and EmbedModel are needed — oos-demo does not start the LLM.
type LLMConfig struct {
	URL        string `toml:"url"`
	EmbedModel string `toml:"embed_model"`
}

// PostgreSQLConfig holds the PostgreSQL connection settings.
type PostgreSQLConfig struct {
	Port     int               `toml:"port"`
	Database string            `toml:"database"`
	User     string            `toml:"user"`
	Password string            `toml:"password"`
	AppUsers map[string]string `toml:"app_users"`
}

// DexConfig holds the Dex identity provider settings.
type DexConfig struct {
	Port  int       `toml:"port"`
	Users []DexUser `toml:"users"`
}

// DexUser is a static Dex login account used in the demo environment.
type DexUser struct {
	Email    string `toml:"email"`
	Password string `toml:"password"`
}

// ServiceConfig holds a single service's network configuration.
type ServiceConfig struct {
	Port int `toml:"port"`
}

// LoadConfig reads demo.toml from the current working directory.
// The demo is meant to be started from the repository root.
func LoadConfig() (*Config, error) {
	path := demoTOMLName

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("demo.toml not found — run oos-demo from the repository root")
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("demo.toml read error: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("demo.toml invalid: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.PostgreSQL.Database == "" {
		return fmt.Errorf("[postgresql] database missing")
	}
	if c.PostgreSQL.User == "" {
		return fmt.Errorf("[postgresql] user missing")
	}
	return nil
}

// ── Directories ───────────────────────────────────────────────────────────────

// BinDir returns the directory where the compiled binaries live.
// The demo is started from the repository root, so this is ./dist.
func (c *Config) BinDir() string {
	return distDirName
}

// DataDir returns the directory for persistent service data.
func (c *Config) DataDir() string {
	return filepath.Join(homeDir(), oosDir, "data")
}

// LogDir returns the directory for service log files.
func (c *Config) LogDir() string {
	return filepath.Join(homeDir(), oosDir, "logs")
}

// ConfigDir returns the directory for generated service config files.
func (c *Config) ConfigDir() string {
	return filepath.Join(homeDir(), oosDir, "config")
}

// EnsureDirs creates all required runtime directories.
//
// BinDir is intentionally not created here — it is expected to exist
// already as the build output directory.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.DataDir(),
		c.LogDir(),
		c.ConfigDir(),
		filepath.Join(c.DataDir(), "postgresql"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	return nil
}

// PostgresDSN builds the PostgreSQL DSN string from config.
func (c *Config) PostgresDSN() string {
	return c.postgresDSN(c.PostgreSQL.Database)
}

// PostgresAdminDSN builds a DSN pointing at the built-in "postgres"
// maintenance database. Used for bootstrap operations that cannot run
// against the target database itself, most notably CREATE DATABASE.
func (c *Config) PostgresAdminDSN() string {
	return c.postgresDSN("postgres")
}

// postgresDSN is the shared DSN builder.
func (c *Config) postgresDSN(dbname string) string {
	if c.PostgreSQL.Password == "" {
		return fmt.Sprintf("host=localhost port=%d user=%s dbname=%s sslmode=disable",
			c.PostgreSQL.Port, c.PostgreSQL.User, dbname)
	}
	return fmt.Sprintf("host=localhost port=%d user=%s password=%s dbname=%s sslmode=disable",
		c.PostgreSQL.Port, c.PostgreSQL.User, c.PostgreSQL.Password, dbname)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}

// platformSuffix returns the suffix that the Makefile appends to
// platform-specific binaries in ./dist (e.g. "macos", "linux_amd64").
//
// macOS binaries are built as a universal lipo bundle, so there is
// no arch split; all other platforms use <os>_<arch>.
func platformSuffix() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS + "_" + runtime.GOARCH
}
