package gql

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/graphql-go/graphql"
)

type ExecuteResult struct {
	Data   interface{} `json:"data,omitempty"`
	Errors []string    `json:"errors,omitempty"`
}

func Execute(queryStr string, variables map[string]interface{}) (*ExecuteResult, error) {
	if variables == nil {
		variables = map[string]interface{}{}
	}
	result := graphql.Do(graphql.Params{
		Schema:         ActiveSchema,
		RequestString:  queryStr,
		VariableValues: variables,
	})

	out := &ExecuteResult{}
	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			out.Errors = append(out.Errors, e.Message)
		}
		return out, fmt.Errorf("graphql errors: %v", out.Errors)
	}
	out.Data = result.Data
	return out, nil
}

func ExecuteJSON(queryStr string, variables map[string]interface{}) ([]byte, error) {
	result, err := Execute(queryStr, variables)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// isEmptyID prüft ob ein id-Wert als "nicht vorhanden" gilt → INSERT.
func isEmptyID(v interface{}) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case string:
		return val == "" || val == "0"
	case float64:
		return val == 0
	case int64:
		return val == 0
	case int:
		return val == 0
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		return s == "" || s == "0"
	}
}

// BuildMutationFromMap baut eine GraphQL Mutation aus einer Daten-Map.
// Wenn id fehlt oder leer/0 → INSERT. Sonst → UPDATE.
func BuildMutationFromMap(contextName string, data map[string]interface{}) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("keine Daten für Mutation")
	}

	fieldTypes     := map[string]string{}
	readonlyFields := map[string]bool{}
	if activeAST != nil {
		for _, ctx := range activeAST.Contexts {
			if ctx.Name == contextName {
				for _, f := range ctx.Fields {
					fieldTypes[f.Name] = f.Type
					if f.Readonly {
						readonlyFields[f.Name] = true
					}
				}
				break
			}
		}
	}

	idVal, hasID := data["id"]
	if !hasID || isEmptyID(idVal) {
		return buildInsertMutation(contextName, data, fieldTypes, readonlyFields)
	}
	return buildUpdateMutation(contextName, data, fieldTypes, readonlyFields, idVal)
}

// buildUpdateMutation baut eine UPDATE Mutation.
func buildUpdateMutation(contextName string, data map[string]interface{}, fieldTypes map[string]string, readonlyFields map[string]bool, idVal interface{}) (string, error) {
	mutName := "update_" + ContextToFieldName(contextName)
	var args         []string
	var returnFields []string

	switch v := idVal.(type) {
	case float64:
		args = append(args, fmt.Sprintf("id: %d", int64(v)))
	case int64:
		args = append(args, fmt.Sprintf("id: %d", v))
	default:
		clean := sanitizeNumber(fmt.Sprintf("%v", idVal))
		var i int64
		fmt.Sscanf(clean, "%d", &i)
		args = append(args, fmt.Sprintf("id: %d", i))
	}

	for k, v := range data {
		returnFields = append(returnFields, k)
		if k == "id" || readonlyFields[k] {
			continue
		}
		args = append(args, formatArg(k, v, fieldTypes[k]))
	}

	return fmt.Sprintf("mutation {\n  %s(%s) {\n    %s\n  }\n}",
		mutName,
		strings.Join(args, ", "),
		strings.Join(returnFields, "\n    "),
	), nil
}

// buildInsertMutation baut eine INSERT Mutation (ohne id).
func buildInsertMutation(contextName string, data map[string]interface{}, fieldTypes map[string]string, readonlyFields map[string]bool) (string, error) {
	mutName := "insert_" + ContextToFieldName(contextName)
	var args         []string
	var returnFields []string

	// id immer im RETURNING
	returnFields = append(returnFields, "id")

	for k, v := range data {
		if k == "id" {
			continue
		}
		returnFields = append(returnFields, k)
		if readonlyFields[k] {
			continue
		}
		args = append(args, formatArg(k, v, fieldTypes[k]))
	}

	if len(args) == 0 {
		return "", fmt.Errorf("keine Felder für Insert")
	}

	return fmt.Sprintf("mutation {\n  %s(%s) {\n    %s\n  }\n}",
		mutName,
		strings.Join(args, ", "),
		strings.Join(returnFields, "\n    "),
	), nil
}

// formatArg formatiert einen einzelnen GraphQL-Argument-String.
func formatArg(k string, v interface{}, hclType string) string {
	strVal := fmt.Sprintf("%v", v)
	switch hclType {
	case "int":
		clean := sanitizeNumber(strVal)
		var i int64
		fmt.Sscanf(clean, "%d", &i)
		return fmt.Sprintf("%s: %d", k, i)
	case "float":
		clean := sanitizeFloat(strVal)
		var f float64
		fmt.Sscanf(clean, "%f", &f)
		return fmt.Sprintf("%s: %f", k, f)
	default:
		switch val := v.(type) {
		case float64:
			if val == float64(int64(val)) {
				return fmt.Sprintf("%s: %d", k, int64(val))
			}
			return fmt.Sprintf("%s: %f", k, val)
		default:
			return fmt.Sprintf("%s: \"%s\"", k, escapeGraphQLString(strVal))
		}
	}
}

func sanitizeNumber(s string) string {
	var b strings.Builder
	for i, c := range s {
		if unicode.IsDigit(c) || (c == '-' && i == 0) {
			b.WriteRune(c)
		}
	}
	return b.String()
}

func sanitizeFloat(s string) string {
	var b strings.Builder
	dotCount := 0
	for i, c := range s {
		if unicode.IsDigit(c) {
			b.WriteRune(c)
		} else if c == '-' && i == 0 {
			b.WriteRune(c)
		} else if c == '.' || c == ',' {
			dotCount++
			b.WriteRune('.')
		}
	}
	result := b.String()
	if dotCount > 1 {
		last := strings.LastIndex(result, ".")
		result = strings.ReplaceAll(result[:last], ".", "") + result[last:]
	}
	return result
}

func escapeGraphQLString(s string) string {
	result := ""
	for _, c := range s {
		switch c {
		case '"':
			result += "\\\""
		case '\\':
			result += "\\\\"
		case '\n':
			result += "\\n"
		default:
			result += string(c)
		}
	}
	return result
}
