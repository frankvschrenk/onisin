package store

import "onisin.com/oos-common/dsl"

// ContextStore is the read interface into the CTX / DSL configuration
// that oosp serves to clients.
type ContextStore interface {
	LoadAll() error

	GetAST(groups []string) (ast *dsl.OOSAst, role string, ok bool)

	GetDSL(id string) (xml string, found bool, err error)

	GetCTXRaw(id string) (xml string, found bool, err error)

	// GetConfigXML returns the xml column of the oos.config row keyed
	// by namespace. Used for themes and other XML-typed config that
	// does not belong in oos.ctx.
	GetConfigXML(namespace string) (xml string, found bool, err error)

	// SetConfigXML upserts the xml column of the oos.config row keyed
	// by namespace.
	SetConfigXML(namespace, xml string) error

	// GetDSLMeta returns the xml column of the oos.oos_dsl_meta row
	// keyed by namespace ('grammar' or 'enrichment'). Found is false
	// when the row does not exist — DSLSchemaStore treats that as a
	// pre-seed state and skips element chunk generation.
	GetDSLMeta(namespace string) (xml string, found bool, err error)

	GetEnvelope(contextName string, content map[string]any) (envelope map[string]any, err error)

	// ContextsByCTXID returns the ContextAst slice produced by parsing the
	// single oos.ctx row with the given id. One row typically yields
	// one or two contexts (e.g. person_list + person_detail). Returns
	// an empty slice if the row does not describe contexts (e.g.
	// global.conf, groups) or if it cannot be parsed.
	ContextsByCTXID(ctxID string) ([]dsl.ContextAst, error)
}
