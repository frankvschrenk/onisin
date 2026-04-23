package helper

// cmd.go — CLI entry point and Cobra command setup.
//
// InitMeta registers all CLI flags and executes the root command.
// The parsed values are written into the global Meta model.

import (
	"bytes"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
	"onisin.com/oos/crypt"
)

// OOSMode is true when the application was started normally (not as a sub-command).
var OOSMode bool

// UnsecureMode disables TLS verification for local development.
var UnsecureMode bool

var mainCmd = &cobra.Command{
	Use:   "oos",
	Short: "onisin OS",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if cmd.Name() == "bridge" {
			return
		}
		configFile, _ := cmd.Flags().GetString("config")
		loadConfigFile(configFile)
	},
	Run: runMain,
}

func runMain(cmd *cobra.Command, args []string) {
	OOSMode = true

	viper.SetDefault("oos.mcp_port", 59125)

	UnsecureMode, _ = cmd.Flags().GetBool("unsecure")

	if UnsecureMode {
		log.Println("[oos] ⚠️  UNSECURE MODE — TLS disabled")
	}

	mcpPort := viper.GetInt("oos.mcp_port")
	Meta.BridgeURL = viper.GetString("oos.bridge_url")
	if Meta.BridgeURL == "" {
		Meta.BridgeURL = fmt.Sprintf("127.0.0.1:%d", mcpPort)
	}

	Meta.ClusterEndpoints = viper.GetStringSlice("cluster.endpoints")
}

// InitMetaFromAppDirs loads oos.toml from the platform config directory and
// returns the populated MetaModel. Used by non-GUI entry points.
func InitMetaFromAppDirs() MetaModel {
	viper.SetDefault("oos.mcp_port", 59125)

	if err := LoadAppDirsConfig(); err != nil {
		log.Printf("[config] appdirs config error: %v", err)
	}

	OOSMode = true

	mcpPort := viper.GetInt("oos.mcp_port")
	Meta.BridgeURL = fmt.Sprintf("127.0.0.1:%d", mcpPort)

	return Meta
}

// InitMeta registers all CLI flags and executes the root command.
// Returns the populated MetaModel.
func InitMeta() MetaModel {
	mainCmd.Flags().BoolP("unsecure", "u", false, "connect to OOSP without TLS (local development)")
	mainCmd.Flags().String("config", "", "path to config file")

	if err := mainCmd.Execute(); err != nil {
		log.Fatalf("fatal: %v", err)
	}

	return Meta
}

// loadConfigFile resolves and reads the configuration from the given path,
// the platform config directory, a local oos.toml, or an encrypted oos.enc.
func loadConfigFile(configFile string) {
	if configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			log.Fatalf("[config] cannot read %q: %v", configFile, err)
		}
		log.Printf("[config] loaded: %s", configFile)
		return
	}

	if !NeedsSetup() {
		if err := LoadAppDirsConfig(); err == nil {
			return
		}
	}

	if _, err := os.Stat("oos.toml"); err == nil {
		viper.SetConfigFile("oos.toml")
		if err := viper.ReadInConfig(); err != nil {
			log.Printf("[config] oos.toml not readable: %v", err)
		} else {
			log.Println("[config] loaded: oos.toml")
		}
		return
	}

	if crypt.IsEncrypted("oos.enc") {
		password := os.Getenv("OOS_PASSWORD")
		if password == "" {
			fmt.Print("Config password: ")
			pw, _ := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println()
			password = string(pw)
		}
		plain, err := crypt.Decrypt("oos.enc", password)
		if err != nil {
			log.Fatalf("[config] oos.enc decrypt: %v", err)
		}
		viper.SetConfigType("toml")
		if err := viper.ReadConfig(bytes.NewReader(plain)); err != nil {
			log.Fatalf("[config] oos.enc parse: %v", err)
		}
		log.Println("[config] loaded: oos.enc")
	}
}
