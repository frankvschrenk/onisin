package boot

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"onisin.com/oos-common/dsl"
	oostheme "onisin.com/oos-common/theme"
	oosui "onisin.com/oos-common/ui"
	fynedsllib "onisin.com/oos-dsl/dsl"
	"onisin.com/oos/helper"
)

var (
	boardWindowsMu sync.Mutex
	boardWindows   = map[string]*boardWindow{}
)

type boardWindow struct {
	key             string
	contextName     string
	query           string
	data            map[string]any
	formData        map[string]any
	currentState    *fynedsllib.State
	win             fyne.Window
	body            *fyne.Container
	headerContainer *fyne.Container
}

func openOrRefreshBoardWindow(contextName string, data map[string]any) {
	id := extractID(data)
	key := registryKey(contextName, id)

	boardWindowsMu.Lock()
	bw, exists := boardWindows[key]
	if !exists {
		bw = &boardWindow{
			key:         key,
			contextName: contextName,
			query:       helper.Stage.LastQuery,
			data:        copyData(data),
		}
		boardWindows[key] = bw
		boardWindowsMu.Unlock()
		bw.open()
	} else {
		bw.data = copyData(data)
		bw.query = helper.Stage.LastQuery
		boardWindowsMu.Unlock()
		bw.render()
		bw.win.RequestFocus()
	}

	if isEntityContext(contextName) {
		helper.ActiveEntityWindow = bw
	}
}

func isEntityContext(contextName string) bool {
	if helper.OOSAst == nil {
		return false
	}
	for _, c := range helper.OOSAst.Contexts {
		if c.Name == contextName {
			return c.Kind == "entity"
		}
	}
	return false
}

func (bw *boardWindow) GetContextName() string { return bw.contextName }

func (bw *boardWindow) GetMergedData() map[string]any { return bw.mergeData() }

func (bw *boardWindow) Reload() { fyne.Do(func() { bw.render() }) }

func (bw *boardWindow) open() {
	title := windowTitle(bw.contextName)
	if id := extractID(bw.data); id != "" {
		title += " #" + id
	}

	bw.win = fyneApp.NewWindow(title)
	bw.win.Resize(fyne.NewSize(1024, 720))

	bw.body = container.NewStack(
		container.NewCenter(widget.NewLabel("Lädt...")),
	)

	headerContainer := container.NewStack()
	content := container.NewBorder(headerContainer, nil, nil, nil, bw.body)
	bw.win.SetContent(content)

	bw.win.SetOnClosed(func() {
		boardWindowsMu.Lock()
		delete(boardWindows, bw.key)
		boardWindowsMu.Unlock()
	})

	bw.headerContainer = headerContainer
	bw.win.Show()
	bw.render()
}

func (bw *boardWindow) buildHeader(screenAttr func(string) bool) fyne.CanvasObject {
	label := widget.NewLabel(windowTitle(bw.contextName))

	var right []fyne.CanvasObject
	ctx := bw.findContext()

	if ctx != nil && ctx.Kind == "entity" {
		// Delete
		if screenAttr("delete") {
			confirmMsg := ""
			for _, act := range ctx.Actions {
				if act.Event == "on_delete" {
					confirmMsg = act.Confirm
					break
				}
			}
			msg := confirmMsg
			right = append(right, widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
				bw.executeDeleteWithConfirm(msg)
			}))
			right = append(right, widget.NewSeparator())
		}
		if screenAttr("save") {
			right = append(right, widget.NewButtonWithIcon("", theme.DocumentSaveIcon(), func() {
				// State-Snapshot bevor Save — damit Benutzer-Eingaben erfasst werden
				if bw.currentState != nil {
					eventBytes, err := fynedsllib.BuildEvent(bw.contextName, "save", bw.currentState)
					if err == nil {
						var formData map[string]interface{}
						if json.Unmarshal(eventBytes, &formData) == nil && len(formData) > 0 {
							bw.formData = formData
						}
					}
				}
				go bw.executeSave()
			}))
		}
	} else if ctx != nil && ctx.Kind == "collection" {
		for _, nav := range ctx.Navigates {
			if nav.Event == "on_new" {
				right = append(right, widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
					bw.handleAction(bw.contextName, "on_new", nil)
				}))
				break
			}
		}
		right = append(right, widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
			go bw.executeRefresh()
		}))
	}

	if screenAttr("exit") || ctx == nil {
		right = append(right, widget.NewButtonWithIcon("", theme.LogoutIcon(), func() {
			bw.win.Close()
		}))
	}

	rightBox := container.NewHBox(right...)
	return container.NewBorder(nil, nil, label, rightBox)
}

func (bw *boardWindow) render() {
	envelope, err := fetchDSLEnvelope(bw.contextName, bw.data)
	if err != nil {
		log.Printf("[board] DSL für %q: %v", bw.contextName, err)
		bw.showError("DSL nicht verfügbar: " + err.Error())
		return
	}

	state := fynedsllib.NewState()
	if jsonBytes, err := json.Marshal(envelope); err == nil {
		if err := state.LoadJSON(jsonBytes); err != nil {
			log.Printf("[board] State laden: %v", err)
		}
	}

	prefix := boardEntityName(bw.contextName)
	for k, v := range bw.formData {
		state.Set(prefix+"."+k, fmt.Sprintf("%v", v))
	}

	dslNode, ok := envelope["dsl"]
	if !ok {
		bw.showError("Kein DSL für: " + bw.contextName)
		return
	}
	nodeBytes, err := json.Marshal(dslNode)
	if err != nil {
		return
	}
	root, err := deserializeNode(nodeBytes)
	if err != nil {
		bw.showError("DSL Fehler: " + err.Error())
		return
	}

	builder := fynedsllib.NewBuilder(state, bw.contextName, bw.handleAction)
	if helper.ActiveTheme != nil {
		builder.SetThemeProvider(oostheme.NewAdapter(helper.ActiveTheme))
	}
	screen := builder.Build(root)
	bw.currentState = state // State merken für Header-Save-Button

	var boardContent fyne.CanvasObject
	if root.Attr("scroll", "") == "true" {
		boardContent = container.NewVScroll(screen)
	} else {
		boardContent = screen
	}

	// Header mit DSL-Screen-Attributen aufbauen
	screenAttr := func(attr string) bool {
		return root.Attr(attr, "") == "true"
	}
	header := bw.buildHeader(screenAttr)
	if bw.headerContainer != nil {
		bw.headerContainer.Objects = []fyne.CanvasObject{header}
		bw.headerContainer.Refresh()
	}

	bw.body.Objects = []fyne.CanvasObject{boardContent}
	bw.body.Refresh()
}

// handleAction ist der fenster-lokale Action-Handler.
func (bw *boardWindow) handleAction(screenID, action string, state *fynedsllib.State) {
	ctx := bw.findContext()
	if ctx == nil {
		log.Printf("[board] Context %q nicht im AST", bw.contextName)
		return
	}

	for _, nav := range ctx.Navigates {
		if nav.Event != action {
			continue
		}
		bw.executeNavigate(nav, state)
		return
	}

	if state != nil {
		eventBytes, err := fynedsllib.BuildEvent(screenID, action, state)
		if err != nil {
			log.Printf("[board] Event bauen: %v", err)
			return
		}
		var formData map[string]interface{}
		if err := json.Unmarshal(eventBytes, &formData); err == nil && len(formData) > 0 {
			bw.formData = formData
		}
		helper.FireBoardEvent(helper.BoardEvent{
			ScreenID: screenID,
			Action:   action,
			JSON:     eventBytes,
		})
	}

	for _, act := range ctx.Actions {
		if act.Event != action {
			continue
		}
		bw.executeAction(act)
		return
	}

	log.Printf("[board] Unbekannte Action %q in Context %q", action, bw.contextName)
}

func (bw *boardWindow) executeNavigate(nav dsl.NavigateAst, state *fynedsllib.State) {
	if nav.BindLocal == "" || nav.BindForeign == "" {
		fyne.Do(func() { openOrRefreshBoardWindow(nav.ToContext, nil) })
		return
	}

	var localVal string
	if state != nil {
		localVal = state.Get("selected." + nav.BindLocal)
		if localVal == "" {
			localVal = state.Get(boardEntityName(bw.contextName) + "." + nav.BindLocal)
		}
	}
	if localVal == "" {
		log.Printf("[board] Navigate %q: kein Wert für %q", nav.Event, nav.BindLocal)
		return
	}

	go func() {
		data, err := bw.queryForNavigate(nav.ToContext, nav.BindForeign, localVal)
		if err != nil {
			log.Printf("[board] Navigate Query: %v", err)
			return
		}
		fyne.Do(func() { openOrRefreshBoardWindow(nav.ToContext, data) })
	}()
}

func (bw *boardWindow) queryForNavigate(toContext, bindForeign, bindValue string) (map[string]any, error) {
	if helper.OOSAst == nil {
		return nil, fmt.Errorf("kein AST geladen")
	}

	var fields []string
	for _, c := range helper.OOSAst.Contexts {
		if c.Name != toContext {
			continue
		}
		if c.Kind == "collection" && len(c.ListFields) > 0 {
			fields = c.ListFields
		} else {
			for _, f := range c.Fields {
				fields = append(fields, f.Name)
			}
		}
		break
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("Context %q nicht im AST", toContext)
	}

	gqlName := strings.ReplaceAll(toContext, ".", "_")
	query := fmt.Sprintf(`{ %s(%s: %s) { %s } }`,
		gqlName, bindForeign, bindValue, strings.Join(fields, " "))

	jsonStr, err := helper.OOSP.Call("oosp_query", map[string]string{
		"context": toContext,
		"query":   query,
	})
	if err != nil {
		return nil, err
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &wrapper); err != nil {
		return nil, fmt.Errorf("JSON: %w", err)
	}
	raw, ok := wrapper[toContext]
	if !ok {
		for _, v := range wrapper {
			raw = v
			break
		}
	}

	entityKey := boardEntityName(toContext)

	var row map[string]any
	if err := json.Unmarshal(raw, &row); err == nil {
		return map[string]any{entityKey: row}, nil
	}
	var rows []any
	if err := json.Unmarshal(raw, &rows); err == nil {
		return map[string]any{"rows": rows}, nil
	}
	return nil, fmt.Errorf("unbekanntes Response-Format")
}

func (bw *boardWindow) executeAction(act dsl.ActionAst) {
	switch act.Type {
	case "save":
		go bw.executeSave()
	case "delete":
		bw.executeDeleteWithConfirm(act.Confirm)
	}
}

func (bw *boardWindow) executeSave() {
	saveData := bw.mergeData()
	if len(saveData) == 0 {
		return
	}

	dataJSON, _ := json.Marshal(saveData)
	result, err := helper.OOSP.Call("oosp_save", map[string]string{
		"context": bw.contextName,
		"data":    string(dataJSON),
	})

	if err != nil {
		helper.FireBoardEvent(helper.BoardEvent{
			ScreenID: bw.contextName,
			Action:   "save_result",
			Error:    err.Error(),
		})
		return
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &wrapper); err == nil {
		for _, raw := range wrapper {
			var saved map[string]any
			if err := json.Unmarshal(raw, &saved); err == nil {
				bw.data = map[string]any{boardEntityName(bw.contextName): saved}
				bw.formData = nil
				fyne.Do(func() { bw.render() })
			}
		}
	}

	helper.FireBoardEvent(helper.BoardEvent{
		ScreenID: bw.contextName,
		Action:   "save_result",
		Result:   fmt.Sprintf("✅ Gespeichert: %s", bw.contextName),
	})
}

// executeRefresh reloads the current collection window from the server.
//
// The refresh button is a deterministic "give me the current data" action:
// it does not replay any LLM-authored query, because the user's mental
// model is "show me what's in the database right now" — not "repeat the
// filter I had last time". For that reason we build the query straight
// from the context's list_fields, the same shape queryForNavigate uses
// when opening a related collection.
//
// Entity contexts have no canonical list query, so refresh is a no-op
// there; the button is only rendered for collections anyway.
func (bw *boardWindow) executeRefresh() {
	ctx := bw.findContext()
	if ctx == nil || ctx.Kind != "collection" || len(ctx.ListFields) == 0 {
		log.Printf("[board] Refresh: %q ist keine Liste mit list_fields", bw.contextName)
		return
	}

	gqlName := strings.ReplaceAll(bw.contextName, ".", "_")
	query := fmt.Sprintf(`{ %s { %s } }`, gqlName, strings.Join(ctx.ListFields, " "))

	jsonStr, err := helper.OOSP.Call("oosp_query", map[string]string{
		"context": bw.contextName,
		"query":   query,
	})
	if err != nil {
		log.Printf("[board] Refresh Query: %v", err)
		return
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &wrapper); err != nil {
		log.Printf("[board] Refresh JSON: %v", err)
		return
	}

	raw, ok := wrapper[bw.contextName]
	if !ok {
		for _, v := range wrapper {
			raw = v
			break
		}
	}

	var rows []interface{}
	if err := json.Unmarshal(raw, &rows); err != nil {
		log.Printf("[board] Refresh rows: %v", err)
		return
	}

	bw.data = map[string]any{"rows": rows}
	bw.query = query
	fyne.Do(func() { bw.render() })
}

func (bw *boardWindow) executeDeleteWithConfirm(confirmMsg string) {
	if confirmMsg == "" {
		confirmMsg = "Datensatz wirklich löschen?"
	}
	id := extractID(bw.data)
	if id == "" {
		return
	}
	fyne.Do(func() {
		oosui.ShowWarningConfirm("Löschen bestätigen", confirmMsg, "Ja", "Nein", func(ok bool) {
			if !ok {
				return
			}
			go bw.executeDelete(id)
		}, bw.win)
	})
}

// executeDelete sends a GraphQL delete mutation for the given id and reacts
// based on the server's response.
//
// On success the window closes — the record is gone and the user's next
// Refresh in the list will reflect that.
//
// On failure we surface the error in the detail window itself via a Fyne
// error dialog. The BoardEvent we fire in parallel routes the same message
// to the dashboard's Activity panel, but the user is looking at the detail
// window when they click Delete — silent failure there is exactly the
// "nothing happens" symptom we had before the /mutation handler was
// implemented on the server.
func (bw *boardWindow) executeDelete(id string) {
	ctxName := strings.ReplaceAll(bw.contextName, ".", "_")
	mutation := fmt.Sprintf(`mutation { delete_%s(id: %s) }`, ctxName, id)

	_, err := helper.OOSP.Call("oosp_mutation", map[string]string{
		"mutation": mutation,
	})

	ev := helper.BoardEvent{
		ScreenID: bw.contextName,
		Action:   "delete_result",
		Result:   fmt.Sprintf("✅ Gelöscht: %s #%s", bw.contextName, id),
	}
	if err != nil {
		ev.Error = err.Error()
	}
	helper.FireBoardEvent(ev)

	if err != nil {
		log.Printf("[board] Delete %q #%s: %v", bw.contextName, id, err)
		fyne.Do(func() {
			dialog.ShowError(err, bw.win)
		})
		return
	}

	fyne.Do(func() { bw.win.Close() })
}

func (bw *boardWindow) mergeData() map[string]any {
	merged := make(map[string]any)
	for _, v := range bw.data {
		if nested, ok := v.(map[string]any); ok {
			for k, val := range nested {
				merged[k] = val
			}
		}
	}
	for k, v := range bw.formData {
		merged[k] = v
	}
	return merged
}

func (bw *boardWindow) findContext() *dsl.ContextAst {
	if helper.OOSAst == nil {
		return nil
	}
	for i := range helper.OOSAst.Contexts {
		if helper.OOSAst.Contexts[i].Name == bw.contextName {
			return &helper.OOSAst.Contexts[i]
		}
	}
	return nil
}

func (bw *boardWindow) showError(msg string) {
	bw.body.Objects = []fyne.CanvasObject{
		container.NewCenter(widget.NewLabel("⚠ " + msg)),
	}
	bw.body.Refresh()
}

func registryKey(contextName, id string) string {
	if id != "" {
		return contextName + ":" + id
	}
	return contextName
}

func windowTitle(contextName string) string {
	title := strings.ReplaceAll(contextName, "_", " ")
	title = strings.ReplaceAll(title, "detail", "Detail")
	title = strings.ReplaceAll(title, "list", "Liste")
	if len(title) > 0 {
		title = strings.ToUpper(title[:1]) + title[1:]
	}
	return title
}

func boardEntityName(contextName string) string {
	name := strings.TrimSuffix(contextName, "_detail")
	name = strings.TrimSuffix(name, "_list")
	return name
}

func extractID(data map[string]any) string {
	for _, v := range data {
		if nested, ok := v.(map[string]any); ok {
			if id, ok := nested["id"]; ok {
				return fmt.Sprintf("%v", id)
			}
		}
	}
	return ""
}

func copyData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out
}
