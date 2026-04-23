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

// loadAndApplyTheme fetches the active theme from oosp and applies it.
//
// Resolution order:
//  1. oos.ctx[theme] as served by oosp — operator-authored theme wins.
//  2. Built-in DefaultTheme for the preferred variant from helper.
//     This keeps the desktop client visually aligned with onisin.com
//     even when the database has no theme row yet.
//
// The user's variant preference (light/dark) is read from helper —
// see helper.PreferredThemeVariant. A runtime switch calls back into
// ApplyTheme below.
func loadAndApplyTheme() {
	variant := helper.PreferredThemeVariant()

	var t *oostheme.OOSTheme

	if helper.OOSP != nil {
		xml, err := helper.OOSP.Call("oosp_load_theme", map[string]string{
			"variant": variant,
		})
		switch {
		case err != nil:
			log.Printf("[boot] theme load: %v — using default", err)
		case xml == "":
			log.Printf("[boot] no theme.%s in oos.config — using default", variant)
		default:
			parsed, perr := oostheme.ParseXML(xml)
			if perr != nil {
				log.Printf("[boot] theme parse: %v — using default", perr)
			} else {
				t = parsed
			}
		}
	}

	if t == nil {
		t = oostheme.DefaultTheme(variant)
	}

	ApplyTheme(t)
}

// ApplyTheme sets t as the active Fyne theme and records it as the
// current theme for subsequent DSL renders. Safe to call from any
// goroutine; the Fyne interaction is marshalled onto the main thread.
func ApplyTheme(t *oostheme.OOSTheme) {
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
