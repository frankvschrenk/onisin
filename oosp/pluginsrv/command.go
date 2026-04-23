package pluginsrv

// command.go — oosp CLI entry point.

import "github.com/spf13/cobra"

// DebugMode enables verbose logging when set via --debug.
var DebugMode bool

// UnsecureMode is accepted for compatibility but ignored — oosp always runs plain HTTP.
var UnsecureMode bool

// NewCommand builds the root cobra command for oosp.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oosp",
		Short: "OOS Plugin Server (REST)",
		Long: `Starts the OOS Plugin REST server.

Configuration via environment variables (OOSP_ prefix):

  Core:
    OOSP_SERVER_ADDR     Listen address            (default: :9100)
    OOSP_DSN             PostgreSQL DSN            (required)

  LLM / Embeddings — OpenAI-compatible API:
    OOSP_LLM_URL         Base URL of the LLM endpoint
                         (default: http://localhost:11434)
    OOSP_LLM_API_KEY     API key — leave empty for local models
    OOSP_EMBED_MODEL     Embedding model name
                         (default: ibm/granite-embedding:107m-multilingual)

  Storage backends:
    OOSP_VECTOR_BACKEND  Vector store backend      (default: pg)

Flags:
  --debug      Verbose logging
  --unsecure   Accepted but ignored`,
		RunE:          func(cmd *cobra.Command, args []string) error { return Run() },
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().BoolVar(&DebugMode, "debug", false, "Verbose logging")
	cmd.Flags().BoolVarP(&UnsecureMode, "unsecure", "u", false, "Accepted but ignored")
	return cmd
}
