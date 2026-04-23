package tools

// ui.go — Board UI mutation tools.
//
// UIChange  — writes an AI preview into the board (no DB write).
// UISave    — persists board data after explicit user confirmation.
// UINew     — opens an empty input screen for a context.
// Render    — renders arbitrary JSON data into the board.

import (
	"encoding/json"
	"fmt"
	"strings"

	"onisin.com/oos-common/gql"
	"onisin.com/oos/helper"
)

// UIChange merges aiJSON into the current board data and renders a preview.
// No database write occurs — the user must call UISave to persist.
func UIChange(contextName, aiJSON string) (string, error) {
	var aiData map[string]interface{}
	if err := json.Unmarshal([]byte(aiJSON), &aiData); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	merged := mergeCurrentData()
	for k, v := range aiData {
		merged[k] = v
	}

	helper.RenderScreenWithForm(contextName, merged, merged)
	return "preview updated — please review and confirm", nil
}

// UISave persists the current board data to the database.
// Must only be called after the user has explicitly confirmed the changes.
func UISave() (string, error) {
	win := helper.ActiveEntityWindow
	if win == nil {
		return "", fmt.Errorf("no active entity window — load a record first")
	}

	contextName := win.GetContextName()
	saveData := win.GetMergedData()
	if len(saveData) == 0 {
		return "", fmt.Errorf("no data in active window")
	}

	if _, err := saveData_(contextName, saveData); err != nil {
		helper.FireBoardEvent(helper.BoardEvent{
			ScreenID: contextName,
			Action:   "save_result",
			Error:    err.Error(),
		})
		return "", err
	}

	win.Reload()

	msg := fmt.Sprintf("saved: %s", contextName)
	helper.FireBoardEvent(helper.BoardEvent{
		ScreenID: contextName,
		Action:   "save_result",
		Result:   msg,
	})
	return msg, nil
}

// UINew opens an empty input screen for contextName.
func UINew(contextName string) (string, error) {
	helper.Stage.CurrentData = nil
	helper.Stage.CurrentFormData = nil
	helper.RenderScreen(contextName, map[string]interface{}{})
	return fmt.Sprintf("new record screen opened: %q", contextName), nil
}

// Render displays data in the board without a database query.
func Render(contextName, jsonStr string) (string, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		var rows []map[string]interface{}
		if err2 := json.Unmarshal([]byte(jsonStr), &rows); err2 != nil {
			return "", fmt.Errorf("invalid JSON: %w", err)
		}
		if len(rows) > 0 {
			data = rows[0]
		}
	}
	helper.RenderScreen(contextName, data)
	return "board updated", nil
}

// saveData_ persists data to the database via OOSP or the local GQL engine.
func saveData_(contextName string, data map[string]interface{}) (map[string]interface{}, error) {
	dataJSON, _ := json.Marshal(data)

	if helper.OOSP != nil {
		result, err := OOSPSave(contextName, string(dataJSON))
		if err != nil {
			return nil, err
		}
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal([]byte(result), &wrapper); err == nil {
			for _, raw := range wrapper {
				var saved map[string]interface{}
				if err := json.Unmarshal(raw, &saved); err == nil {
					return saved, nil
				}
			}
		}
		return nil, nil
	}

	mutation, err := gql.BuildMutationFromMap(contextName, data)
	if err != nil {
		return nil, fmt.Errorf("mutation build: %w", err)
	}
	res, err := gql.Execute(mutation, nil)
	if err != nil {
		return nil, err
	}
	if dataMap, ok := res.Data.(map[string]interface{}); ok {
		for _, v := range dataMap {
			if row, ok := v.(map[string]interface{}); ok {
				return row, nil
			}
		}
	}
	return nil, nil
}

// mergeCurrentData flattens the current stage data and form data into a single map.
func mergeCurrentData() map[string]interface{} {
	merged := make(map[string]interface{})

	for _, v := range helper.Stage.CurrentData {
		if nested, ok := v.(map[string]interface{}); ok {
			for nk, nv := range nested {
				merged[nk] = nv
			}
		}
	}
	for k, v := range helper.Stage.CurrentFormData {
		merged[k] = v
	}
	return merged
}

// Delete permanently removes a record after explicit user confirmation.
func Delete(contextName, id string) (string, error) {
	if !hasPermission(contextName) {
		return "", fmt.Errorf("role %q has no delete permission on %q",
			helper.ActiveRole, contextName)
	}

	mutation := fmt.Sprintf(`mutation { delete_%s(id: %s) }`,
		strings.ReplaceAll(contextName, ".", "_"), id)

	if helper.OOSP != nil {
		if _, err := OOSPMutation(mutation); err != nil {
			return "", err
		}
	} else {
		if _, err := gql.Execute(mutation, nil); err != nil {
			return "", err
		}
	}

	helper.Stage.CurrentData = nil
	helper.Stage.CurrentFormData = nil
	helper.ClearScreen()

	msg := fmt.Sprintf("deleted: %s #%s", contextName, id)
	helper.FireBoardEvent(helper.BoardEvent{
		ScreenID: contextName,
		Action:   "delete_result",
		Result:   msg,
	})
	return msg, nil
}
