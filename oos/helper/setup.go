package helper

// setup.go — Application configuration load and save.
//
// Reads and writes oos.toml in the platform-specific user config directory.
// In production, values come from environment variables or a secrets manager.

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"onisin.com/oos-common/llm"
	"onisin.com/oos/appdirs"
)

// SetupConfig holds all user-configurable connection parameters.
type SetupConfig struct {
	OOSPUrl      string
	DexURL       string
	VaultURL     string
	ClientID     string
	LLMAddr      string
	LLMApiKey    string
	LLMChatModel string
}

// ConfigPath returns the platform-specific path to oos.toml.
func ConfigPath() string {
	dir := appdirs.New("oos", "").UserConfig()
	return filepath.Join(dir, "oos.toml")
}

// NeedsSetup returns true when no oos.toml exists yet.
func NeedsSetup() bool {
	_, err := os.Stat(ConfigPath())
	return os.IsNotExist(err)
}

// LoadAppDirsConfig reads oos.toml and applies all values to the global
// Meta and LLM variables. Environment variables with the OOS_ prefix override
// file values.
func LoadAppDirsConfig() error {
	path := ConfigPath()

	viper.SetConfigFile(path)
	viper.SetEnvPrefix("OOS")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("oos.toml read error: %w", err)
	}

	if v := viper.GetString("oosp.url"); v != "" {
		Meta.OOSPUrl = v
	}
	if v := viper.GetString("dex.url"); v != "" {
		Meta.IAM.IssuerURL = v
	}
	if v := viper.GetString("dex.client_id"); v != "" {
		Meta.IAM.ClientID = v
	}
	if v := viper.GetString("vault.url"); v != "" {
		Meta.Vault.URL = v
	}
	if v := viper.GetString("llm.url"); v != "" {
		llm.URL = v
	}
	if v := viper.GetString("llm.api_key"); v != "" {
		llm.APIKey = v
	}
	// Viper sometimes misreads TOML keys with underscores — try both variants.
	if v := viper.GetString("llm.chat_model"); v != "" {
		llm.ChatModel = v
	} else if v := viper.GetString("llm.chat-model"); v != "" {
		llm.ChatModel = v
	}
	if v := viper.GetString("llm.schema_strategy"); v != "" {
		llm.SchemaStrategy = v
	}

	applyThemeVariantFromViper()

	log.Printf("[config] loaded: %s", path)
	log.Printf("[config] OOSP: %s  LLM: %s  chat-model: %s  api-key: %s",
		Meta.OOSPUrl, llm.URL, llm.ChatModel, maskKey(llm.APIKey))

	return nil
}

// SaveConfig writes cfg to oos.toml and updates the in-memory globals.
// It reads the existing file first so that keys not managed by the dialog
// are preserved rather than silently dropped.
func SaveConfig(cfg SetupConfig) error {
	path := ConfigPath()

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")

	// Read existing file so unknown keys are preserved.
	// Ignore read errors — the file may not exist yet on first save.
	_ = v.ReadInConfig()

	// Always write all dialog fields, even when empty, so the user can
	// deliberately clear a value.
	v.Set("oosp.url", cfg.OOSPUrl)
	v.Set("dex.url", cfg.DexURL)
	v.Set("dex.client_id", cfg.ClientID)
	v.Set("vault.url", cfg.VaultURL)
	v.Set("llm.url", cfg.LLMAddr)
	v.Set("llm.api_key", cfg.LLMApiKey)
	v.Set("llm.chat_model", cfg.LLMChatModel)

	if err := v.WriteConfig(); err != nil {
		// WriteConfig fails when the file does not exist yet — use SafeWriteConfig.
		if err2 := v.SafeWriteConfig(); err2 != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}

	// Update in-memory globals so the running app sees the new values
	// without requiring a restart.
	Meta.OOSPUrl = cfg.OOSPUrl
	Meta.IAM.IssuerURL = cfg.DexURL
	Meta.IAM.ClientID = cfg.ClientID
	Meta.Vault.URL = cfg.VaultURL
	llm.URL = cfg.LLMAddr
	llm.APIKey = cfg.LLMApiKey
	llm.ChatModel = cfg.LLMChatModel

	log.Printf("[config] saved: %s", path)
	return nil
}

// DefaultSetupConfig returns a SetupConfig pre-filled with current values,
// using localhost defaults where nothing is configured yet.
func DefaultSetupConfig() SetupConfig {
	return SetupConfig{
		OOSPUrl:      orDefault(Meta.OOSPUrl, "http://localhost:9100"),
		DexURL:       orDefault(Meta.IAM.IssuerURL, "http://localhost:5556"),
		VaultURL:     orDefault(Meta.Vault.URL, "http://localhost:8200"),
		ClientID:     orDefault(Meta.IAM.ClientID, "oos-desktop"),
		LLMAddr:      orDefault(llm.URL, "http://localhost:11434"),
		LLMApiKey:    llm.APIKey,
		LLMChatModel: llm.ChatModel,
	}
}

// PingResult holds the result of a connectivity check.
type PingResult struct {
	OOSP string
}

// PingOOSP tests whether the given OOSP endpoint is reachable.
func PingOOSP(oospURL string) PingResult {
	if oospURL == "" {
		return PingResult{OOSP: "no endpoint configured"}
	}
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := client.Get(oospURL)
	if err != nil {
		return PingResult{OOSP: fmt.Sprintf("not reachable: %v", err)}
	}
	defer resp.Body.Close()
	return PingResult{OOSP: "ok"}
}

// maskKey shows only the first 8 characters of an API key in log output.
func maskKey(key string) string {
	if key == "" {
		return "(empty)"
	}
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "..."
}

func orDefault(val, def string) string {
	if val != "" {
		return val
	}
	return def
}
