package helper

// ctx.go — OOS schema (AST) loading.
//
// The AST is always fetched from OOSP. There is no local file fallback —
// CTX files live exclusively in PostgreSQL (oos.ctx), managed by oosp.

import (
	"log"

	"onisin.com/oos-common/dsl"
)

// OOSAst is the active schema loaded from OOSP.
var OOSAst *dsl.OOSAst

// OOSPFetchASTFn is wired up by boot/oosp_connect.go after the OOSP connection
// is established. It fetches the current AST from the OOSP REST API.
var OOSPFetchASTFn func() (*dsl.OOSAst, error)

// InitCtx loads the AST from OOSP and stores it in OOSAst.
// Returns an empty AST when OOSP is not yet connected (pre-seed state).
func InitCtx() *dsl.OOSAst {
	if OOSPFetchASTFn != nil && Meta.OOSPUrl != "" {
		ast, err := OOSPFetchASTFn()
		if err != nil {
			log.Printf("[ctx] OOSP fetch failed: %v — starting with empty AST", err)
		} else {
			OOSAst = ast
			log.Printf("[ctx] AST from OOSP: %d contexts", len(OOSAst.Contexts))
			return OOSAst
		}
	}

	log.Println("[ctx] OOSP not connected — starting with empty AST")
	OOSAst = &dsl.OOSAst{}
	return OOSAst
}

// ReloadCtx re-fetches the AST from OOSP and refreshes the current board view.
func ReloadCtx() {
	ast := InitCtx()
	if ast == nil {
		return
	}
	log.Printf("[ctx] reloaded: %d contexts", len(ast.Contexts))
	if RenderFn != nil && Stage.CurrentContext != "" {
		RenderFn(Stage.CurrentContext)
	}
}

// ClearCachedSession clears all session-related identity state.
func ClearCachedSession() error {
	ActiveIDToken = ""
	ActiveGroups = nil
	ActiveRole = ""
	activeEmail = ""
	activeUsername = ""
	return nil
}
