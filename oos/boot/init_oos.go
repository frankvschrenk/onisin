package boot

// init_oos.go — OOS initialisation after successful login.
//
// Loads the AST from OOSP, builds the GraphQL schema, resolves the active
// role and applies the configured UI theme.

import (
	"log"

	"fyne.io/fyne/v2"
	"onisin.com/oos-common/gql"
	oostheme "onisin.com/oos-common/theme"
	"onisin.com/oos/helper"
)

// initOOS bootstraps the application after the user session is established.
// Must be called once OOSP is connected and JWT claims are applied.
//
// Order matters here: we push the active group to the OOSP client *before*
// the first AST fetch, so oosp already resolves the caller's role on that
// very first call. Without this the AST would come back under the oos-admin
// default and later role-scoped operations would silently run with elevated
// rights.
func initOOS() {
	if helper.OOSP != nil {
		helper.OOSP.SetActiveGroup(helper.ActiveGroupForOOSP())
	}

	ast := helper.InitCtx()

	helper.DsnRegistry = helper.InitDsnFromAST(ast)
	if err := gql.BuildSchema(ast, helper.DsnRegistry); err != nil {
		log.Printf("[boot] GQL schema: %v", err)
	}

	helper.ActiveRole = helper.ResolveRole(ast, helper.ActiveGroups)
	if helper.ActiveRole != "" {
		log.Printf("[boot] role: %s (groups: %v)", helper.ActiveRole, helper.ActiveGroups)
	}

	log.Printf("[boot] ✅ OOS ready — %d contexts", len(ast.Contexts))
	log.Printf("[boot] LLM endpoint: %s", helper.LLMUrl)

	loadAndApplyTheme()
}

// loadAndApplyTheme fetches the active theme from OOSP and applies it to Fyne.
// If no theme is configured, the default Fyne theme remains active.
func loadAndApplyTheme() {
	if helper.OOSP == nil {
		return
	}

	xml, err := helper.OOSP.Call("oosp_load_theme", nil)
	if err != nil {
		log.Printf("[boot] theme load: %v", err)
		return
	}
	if xml == "" {
		log.Printf("[boot] no theme configured — using default theme")
		return
	}

	t, err := oostheme.ParseXML(xml)
	if err != nil {
		log.Printf("[boot] theme parse: %v", err)
		return
	}

	helper.ActiveTheme = t

	fyne.Do(func() {
		fyneApp := fyne.CurrentApp()
		if fyneApp == nil {
			return
		}
		fyneApp.Settings().SetTheme(oostheme.NewGlobalFyneTheme(t))
		log.Printf("[boot] theme applied — variant: %s", t.Variant)
	})
}
