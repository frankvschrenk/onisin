package tools

// query.go — OOS data query and render tool.
//
// Loads data via OOSP (or local GraphQL fallback) and renders it
// into the active board window.

import (
	"encoding/json"
	"fmt"
	"strings"

	"onisin.com/oos-common/gql"
	"onisin.com/oos/helper"
)

// Query loads data for contextName using queryStr and renders it to the board.
// It is the primary read tool exposed to the AI.
func Query(contextName, queryStr string) (string, error) {
	data, err := executeQuery(contextName, queryStr)
	if err != nil {
		return "", err
	}

	helper.Stage.LastQuery = queryStr
	helper.RenderScreen(contextName, data)
	return fmt.Sprintf("board updated: %q", contextName), nil
}

// executeQuery runs queryStr against OOSP when connected, otherwise falls back
// to the local GraphQL engine.
func executeQuery(contextName, queryStr string) (map[string]interface{}, error) {
	if helper.OOSP != nil {
		return queryViaOOSP(contextName, queryStr)
	}
	return queryViaGQL(queryStr)
}

// queryViaOOSP proxies the query to the OOSP server and parses the JSON response.
func queryViaOOSP(contextName, queryStr string) (map[string]interface{}, error) {
	jsonStr, err := OOSPQuery(contextName, queryStr)
	if err != nil {
		return nil, fmt.Errorf("OOSP query: %w", err)
	}

	trimmed := strings.TrimSpace(jsonStr)
	if !strings.HasPrefix(trimmed, "{") {
		return nil, fmt.Errorf("%s", trimmed)
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &wrapper); err != nil {
		return nil, fmt.Errorf("OOSP JSON parse: %w", err)
	}

	raw, ok := wrapper[contextName]
	if !ok {
		for _, v := range wrapper {
			raw = v
			break
		}
	}

	var row map[string]interface{}
	if err := json.Unmarshal(raw, &row); err == nil {
		return map[string]interface{}{entityName(contextName): row}, nil
	}

	var rows []interface{}
	if err := json.Unmarshal(raw, &rows); err == nil {
		return map[string]interface{}{"rows": rows}, nil
	}

	return map[string]interface{}{}, nil
}

// queryViaGQL executes the query against the local GraphQL engine.
func queryViaGQL(queryStr string) (map[string]interface{}, error) {
	result, err := gql.Execute(queryStr, nil)
	if err != nil {
		return nil, fmt.Errorf("GQL: %w", err)
	}
	dataMap, ok := result.Data.(map[string]interface{})
	if !ok || len(dataMap) == 0 {
		return nil, fmt.Errorf("no data from GQL server")
	}
	for _, val := range dataMap {
		if row, ok := val.(map[string]interface{}); ok {
			return row, nil
		}
	}
	return dataMap, nil
}

// entityName strips the _list or _detail suffix from a context name.
func entityName(contextName string) string {
	name := strings.TrimSuffix(contextName, "_detail")
	name = strings.TrimSuffix(name, "_list")
	return name
}
