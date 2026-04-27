package pluginsrv

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"onisin.com/oos-common/db"
	"onisin.com/oos-common/gql"
	"onisin.com/oos-common/oosp"
	"onisin.com/oos-common/plugin"
	"onisin.com/oosp/api"
	"onisin.com/oosp/pluginsrv/store"
)

type oospConfig struct {
	Addr        string
	DSN         string
	Datasources map[string]db.DatasourceConfig

	VectorBackend string
	VectorAddr    string

	// LLMUrl is the base URL of the OpenAI-compatible endpoint used for embeddings.
	// e.g. http://localhost:11434  (Ollama)
	LLMUrl     string
	LLMApiKey  string
	EmbedModel string
}

var cfg oospConfig
var dsnRegistry map[string]any
var activeStore  store.ContextStore
var activeVector store.VectorStore
var activeEmbed  store.EmbedStore
var activeSchema *store.SchemaStore
var activeDSLSchema *store.DSLSchemaStore

func Run() error {
	applyConfig()

	store.DebugStore = DebugMode

	if err := initStore(); err != nil {
		return err
	}
	if err := initDatasources(); err != nil {
		return err
	}

	initVectorStore()
	initEmbedStore()
	initSchemaStore()
	initDSLSchemaStore()
	
	// Initialize event system (from server_enhanced.go)
	initEventSystem()

	return startRESTServer(cfg.Addr)
}

func initStore() error {
	if cfg.DSN == "" {
		log.Println("[oosp] ⚠️  OOSP_DSN not set — no store")
		return nil
	}
	s, err := store.New(cfg.DSN)
	if err != nil {
		return fmt.Errorf("store init: %w", err)
	}
	if err := s.LoadAll(); err != nil {
		return fmt.Errorf("store load: %w", err)
	}
	activeStore = s
	log.Println("[oosp] ✅ store: PostgreSQL")
	return nil
}

func initDatasources() error {
	plugin.HTTPClientFactory = func(url string) (plugin.Caller, error) {
		return oosp.NewHTTP(url)
	}

	if len(cfg.Datasources) == 0 {
		log.Println("[oosp] ⚠️  Keine Datasources konfiguriert")
		return nil
	}

	dsnRegistry = make(map[string]any)
	for name, dsCfg := range cfg.Datasources {
		client, err := db.Connect(dsCfg, "", "")
		if err != nil {
			log.Printf("[oosp] ❌ datasource %q: %v", name, err)
			continue
		}
		dsnRegistry[name] = client
		log.Printf("[oosp] ✅ datasource: %s (%s/%s)", name, dsCfg.Type, dsCfg.Database)
	}

	if len(dsnRegistry) == 0 {
		return fmt.Errorf("keine Datasource-Verbindung aufgebaut")
	}

	// Build GraphQL schema — skip gracefully when AST is empty (pre-seed state).
	if activeStore != nil {
		ast, _, ok := activeStore.GetAST([]string{"oos-admin"})
		if ok && ast != nil && len(ast.Contexts) > 0 {
			if err := gql.BuildSchema(ast, dsnRegistry); err != nil {
				log.Printf("[oosp] ⚠️  GraphQL schema: %v", err)
			}
		} else {
			log.Println("[oosp] GraphQL schema skipped — AST empty (run --seed)")
		}
	}

	return nil
}

func initVectorStore() {
	if cfg.VectorBackend == "" && cfg.VectorAddr == "" {
		log.Println("[oosp] Vector Store nicht konfiguriert — übersprungen")
		return
	}
	vs, err := store.NewVectorStore(cfg.VectorBackend, cfg.VectorAddr)
	if err != nil {
		log.Printf("[oosp] ⚠️  Vector Store: %v", err)
		return
	}
	activeVector = vs
	log.Printf("[oosp] ✅ Vector Store: %s @ %s", cfg.VectorBackend, cfg.VectorAddr)
}

// initEmbedStore initialises the OpenAI-compatible embedding store.
func initEmbedStore() {
	if cfg.LLMUrl == "" && cfg.EmbedModel == "" {
		log.Println("[oosp] embed store not configured — skipping")
		return
	}
	es, err := store.NewEmbedStore(cfg.LLMUrl, cfg.LLMApiKey, cfg.EmbedModel)
	if err != nil {
		log.Printf("[oosp] ⚠️  embed store: %v", err)
		return
	}
	activeEmbed = es
	log.Printf("[oosp] ✅ embed store: %s (model=%s)", cfg.LLMUrl, cfg.EmbedModel)
}

// initSchemaStore wires up the SchemaStore, runs a backfill and starts
// the pg_notify listener so oos.oos_ctx_schema stays current.
//
// The SchemaStore needs the ContextStore so it can translate oos.ctx row
// ids into ContextAst slices for chunk rendering — the same AST the
// GraphQL schema uses, not a second parse of the raw XML.
func initSchemaStore() {
	if activeEmbed == nil {
		log.Println("[oosp] schema store skipped — no embed store configured")
		return
	}
	if cfg.DSN == "" {
		log.Println("[oosp] schema store skipped — no DSN")
		return
	}
	if activeStore == nil {
		log.Println("[oosp] schema store skipped — no context store")
		return
	}

	// Open a dedicated DB connection for the schema store.
	// The ContextStore uses its own connection; this one is for schema ops.
	schemaDB, err := openDB(cfg.DSN)
	if err != nil {
		log.Printf("[oosp] ⚠️  schema store db: %v", err)
		return
	}

	activeSchema = store.NewSchemaStore(schemaDB, activeStore, activeEmbed)

	// onCTXChange is called by SchemaStore before every chunk re-render.
	// It reloads the AST and rebuilds the GraphQL schema so queries work
	// immediately after --seed without restarting oosp, and so the AST
	// the chunk renderer reads is already up to date.
	onCTXChange := func() {
		if ps, ok := activeStore.(*store.PostgresStore); ok {
			if err := ps.Reload(); err != nil {
				log.Printf("[oosp] AST reload: %v", err)
				return
			}
		}
		ast, _, ok := activeStore.GetAST([]string{"oos-admin"})
		if ok && ast != nil && len(ast.Contexts) > 0 {
			if err := gql.BuildSchema(ast, dsnRegistry); err != nil {
				log.Printf("[oosp] GraphQL rebuild: %v", err)
			} else {
				log.Printf("[oosp] ✅ GraphQL schema rebuilt (%d contexts)", len(ast.Contexts))
			}
		}
	}
	activeSchema.SetOnChange(onCTXChange)

	// Backfill: embed any CTX rows that are not yet in oos.oos_ctx_schema.
	go func() {
		if err := activeSchema.Backfill(); err != nil {
			log.Printf("[oosp] schema backfill: %v", err)
		}
	}()

	// Listen for CTX changes and re-embed on the fly.
	go activeSchema.ListenForCTXChanges(context.Background(), cfg.DSN)

	log.Println("[oosp] ✅ schema store ready")
}

// initDSLSchemaStore wires up the DSLSchemaStore: regenerate element
// chunks from the XSD in oos.config, backfill pattern chunks from
// oos.dsl, then listen for oos_dsl_notify to keep patterns current.
//
// Shares the same embed store as the CTX schema pipeline so queries
// can mix element and pattern chunks in a single vector space.
func initDSLSchemaStore() {
	if activeEmbed == nil {
		log.Println("[oosp] dsl schema store skipped — no embed store configured")
		return
	}
	if cfg.DSN == "" {
		log.Println("[oosp] dsl schema store skipped — no DSN")
		return
	}
	if activeStore == nil {
		log.Println("[oosp] dsl schema store skipped — no context store")
		return
	}

	// Dedicated DB connection for the DSL schema store. Keeping it
	// separate from the CTX schema store's connection prevents one
	// slow embed call from blocking the other listener.
	dslDB, err := openDB(cfg.DSN)
	if err != nil {
		log.Printf("[oosp] ⚠️  dsl schema store db: %v", err)
		return
	}

	activeDSLSchema = store.NewDSLSchemaStore(dslDB, activeStore, activeEmbed)

	// Backfill in a goroutine so startup isn't blocked by embedding
	// latency. The XSD element pass embeds ~30 short chunks, which
	// with Ollama can take a few seconds per restart.
	go func() {
		if err := activeDSLSchema.Backfill(); err != nil {
			log.Printf("[oosp] dsl schema backfill: %v", err)
		}
	}()

	// Listen for DSL changes and re-embed the affected pattern.
	go activeDSLSchema.ListenForDSLChanges(context.Background(), cfg.DSN)

	log.Println("[oosp] ✅ dsl schema store ready")
}

func startRESTServer(addr string) error {
	svc := &api.Services{
		GetAST:          GetAST,
		ExecuteQuery:    ExecuteQuery,
		ExecuteMutation: ExecuteMutation,
		ExecuteSave:     ExecuteSave,
		GetDSL:          GetDSL,
		GetEnvelope:     GetEnvelope,
		Embed:           Embed,
		VectorUpsert:    VectorUpsert,
		VectorSearch:    VectorSearch,
		GetTheme:        GetTheme,
		SetTheme:        SetTheme,
		GetDSLMeta:      GetDSLMeta,
		SchemaSearch: func(query string, n int) (any, error) {
			return SchemaSearch(query, n)
		},
		SchemaAll: func() (any, error) {
			return SchemaAll()
		},
		DSLSchemaSearch: func(query string, n int) (any, error) {
			return DSLSchemaSearch(query, n)
		},
		DSLSchemaAll: func() (any, error) {
			return DSLSchemaAll()
		},
		// Event System APIs
		EventSearch: func(mapping, query, streamID string, limit int) (any, error) {
			return EventSearch(mapping, query, streamID, limit)
		},
		EventMappings: func() (any, error) {
			return EventMappings()
		},
		EventStreams: func(mapping string, limit int) (any, error) {
			return EventStreams(mapping, limit)
		},
	}
	s := api.New(addr, svc)
	if err := s.Start(); err != nil {
		return err
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	
	// Graceful shutdown with event system cleanup
	log.Println("[oosp] shutting down...")
	shutdownEventSystem()
	log.Println("[oosp] shutdown complete")
	return nil
}
