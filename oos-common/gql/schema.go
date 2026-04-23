package gql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"
	"onisin.com/oos-common/dsl"
	"onisin.com/oos-common/plugin"
)

var ActiveSchema graphql.Schema
var activeAST *dsl.OOSAst

func BuildSchema(ast *dsl.OOSAst, registry map[string]any) error {
	queryFields    := graphql.Fields{}
	mutationFields := graphql.Fields{}

	for _, ctx := range ast.Contexts {
		isList   := ctx.Kind == "collection"
		objType  := buildObjectTypeFromAST(ctx)
		queryName := ctx.GQLQuery

		conn := registryEntry(ctx, registry)

		switch c := conn.(type) {
		case *sql.DB:
			queryFields[queryName] = buildQueryFieldFromAST(ctx, objType, isList, c)
			if !isList {
				mutationFields["update_"+queryName] = buildMutationFieldFromAST(ctx, objType, c)
				mutationFields["insert_"+queryName] = buildInsertFieldFromAST(ctx, objType, c)
				mutationFields["delete_"+queryName] = buildDeleteFieldFromAST(ctx, c)
			}
		case plugin.Caller:
			queryFields[queryName] = buildPluginQueryField(ctx, objType, isList, c)
			if !isList {
				mutationFields["update_"+queryName] = buildPluginMutationField(ctx, objType, c)
				mutationFields["delete_"+queryName] = buildPluginDeleteField(ctx, c)
			}
		default:
			continue
		}
	}

	if len(queryFields) == 0 {
		ActiveSchema = graphql.Schema{}
		activeAST = ast
		return nil
	}

	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Query",
			Fields: queryFields,
		}),
		Mutation: graphql.NewObject(graphql.ObjectConfig{
			Name:   "Mutation",
			Fields: mutationFields,
		}),
	})
	if err != nil {
		return fmt.Errorf("schema build error: %w", err)
	}

	ActiveSchema = schema
	activeAST = ast
	return nil
}

func registryEntry(ctx dsl.ContextAst, registry map[string]any) any {
	dsnName := ctx.DSN
	if dsnName == "" {
		dsnName = ctx.Source
	}
	return registry[dsnName]
}

// buildInsertFieldFromAST baut ein GraphQL Insert-Feld — kein id-Argument, RETURNING id.
func buildInsertFieldFromAST(ctx dsl.ContextAst, objType *graphql.Object, db *sql.DB) *graphql.Field {
	args := graphql.FieldConfigArgument{}
	for _, f := range ctx.Fields {
		if f.Readonly || f.Name == "id" {
			continue
		}
		args[f.Name] = &graphql.ArgumentConfig{Type: typeToGraphQL(f.Type)}
	}

	source := ctx.Source
	fields := ctx.Fields

	return &graphql.Field{
		Type: objType,
		Args: args,
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			return executeInsert(db, source, fields, p.Args)
		},
	}
}

// executeInsert baut und führt ein INSERT ... RETURNING aus.
func executeInsert(db *sql.DB, source string, fields []dsl.FieldAst, args map[string]interface{}) (interface{}, error) {
	readonlyFields := map[string]bool{}
	fieldSet       := map[string]bool{}
	for _, f := range fields {
		fieldSet[f.Name] = true
		if f.Readonly {
			readonlyFields[f.Name] = true
		}
	}

	var cols   []string
	var placeholders []string
	var values []interface{}
	i := 1

	for key, val := range args {
		if !fieldSet[key] || readonlyFields[key] || key == "id" {
			continue
		}
		cols = append(cols, key)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		values = append(values, val)
		i++
	}

	if len(cols) == 0 {
		return nil, fmt.Errorf("keine Felder für Insert")
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
		source,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		selectColumns(fields),
	)

	rows, err := db.Query(query, values...)
	if err != nil {
		return nil, fmt.Errorf("insert fehlgeschlagen: %w", err)
	}
	defer rows.Close()

	results, err := scanRows(rows)
	if err != nil || len(results) == 0 {
		return nil, err
	}
	return results[0], nil
}

func buildPluginQueryField(ctx dsl.ContextAst, objType *graphql.Object, isList bool, caller plugin.Caller) *graphql.Field {
	var returnType graphql.Output
	if isList {
		returnType = graphql.NewList(objType)
	} else {
		returnType = objType
	}

	args   := graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.String}}
	source := ctx.Source

	return &graphql.Field{
		Type: returnType,
		Args: args,
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			toolArgs := map[string]string{}
			if id, ok := p.Args["id"]; ok {
				toolArgs["id"] = fmt.Sprintf("%v", id)
			}
			raw, err := caller.Call(source, toolArgs)
			if err != nil {
				return nil, fmt.Errorf("plugin %q: %w", source, err)
			}
			var result interface{}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				return nil, fmt.Errorf("plugin %q: ungültiges JSON: %w", source, err)
			}
			if isList {
				if v, ok := result.([]interface{}); ok {
					return v, nil
				}
				return []interface{}{result}, nil
			}
			return result, nil
		},
	}
}

func buildPluginMutationField(ctx dsl.ContextAst, objType *graphql.Object, caller plugin.Caller) *graphql.Field {
	args := graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.Int}}
	for _, f := range ctx.Fields {
		if f.Readonly || f.Name == "id" {
			continue
		}
		args[f.Name] = &graphql.ArgumentConfig{Type: typeToGraphQL(f.Type)}
	}

	saveTool := ctx.Save
	if saveTool == "" {
		saveTool = ctx.Source + "_save"
	}

	return &graphql.Field{
		Type: objType,
		Args: args,
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			dataJSON, err := json.Marshal(p.Args)
			if err != nil {
				return nil, err
			}
			raw, err := caller.Call(saveTool, map[string]string{"data": string(dataJSON)})
			if err != nil {
				return nil, fmt.Errorf("plugin save %q: %w", saveTool, err)
			}
			var result interface{}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				return nil, err
			}
			return result, nil
		},
	}
}

// buildObjectTypeFromAST builds a GraphQL object type for a context.
// For collection contexts (isList=true), only the fields listed in ListFields
// are exposed — this matches exactly what the SQL query returns and prevents
// callers from requesting fields that are not part of the list view.
func buildObjectTypeFromAST(ctx dsl.ContextAst) *graphql.Object {
	gqlFields := graphql.Fields{}

	if ctx.Kind == "collection" && len(ctx.ListFields) > 0 {
		// Only expose the declared list_fields for collection contexts.
		for _, name := range ctx.ListFields {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			// Find the field type from the full field list.
			gqlType := graphql.String // default
			for _, f := range ctx.Fields {
				if f.Name == name {
					gqlType = typeToGraphQL(f.Type)
					break
				}
			}
			gqlFields[name] = &graphql.Field{Type: gqlType}
		}
	} else {
		// Entity contexts expose all fields.
		for _, f := range ctx.Fields {
			gqlFields[f.Name] = &graphql.Field{Type: typeToGraphQL(f.Type)}
		}
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   contextToTypeName(ctx.Name),
		Fields: gqlFields,
	})
}

func buildQueryFieldFromAST(ctx dsl.ContextAst, objType *graphql.Object, isList bool, db *sql.DB) *graphql.Field {
	var returnType graphql.Output
	if isList {
		returnType = graphql.NewList(objType)
	} else {
		returnType = objType
	}

	args := graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.Int}}
	for _, f := range ctx.Fields {
		if !f.Filterable {
			continue
		}
		gqlType := typeToGraphQL(f.Type)
		args[f.Name] = &graphql.ArgumentConfig{Type: gqlType}
		if f.Type == "int" || f.Type == "float" {
			args[f.Name+"_gt"] = &graphql.ArgumentConfig{Type: gqlType}
			args[f.Name+"_lt"] = &graphql.ArgumentConfig{Type: gqlType}
		}
		if f.Type == "string" || f.Type == "" {
			args[f.Name+"_like"] = &graphql.ArgumentConfig{Type: graphql.String}
		}
	}

	source := ctx.Source
	fields := ctx.Fields
	listFields := ctx.ListFields // captured for the resolver closure

	return &graphql.Field{
		Type: returnType,
		Args: args,
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			var conditions []string
			var values     []interface{}
			i := 1

			if id, ok := p.Args["id"]; ok {
				conditions = append(conditions, fmt.Sprintf("id = $%d", i))
				values = append(values, id)
				i++
			}
			for _, f := range fields {
				if !f.Filterable {
					continue
				}
				if val, ok := p.Args[f.Name]; ok {
					conditions = append(conditions, fmt.Sprintf("%s = $%d", f.Name, i))
					values = append(values, val)
					i++
				}
				if val, ok := p.Args[f.Name+"_gt"]; ok {
					conditions = append(conditions, fmt.Sprintf("%s > $%d", f.Name, i))
					values = append(values, val)
					i++
				}
				if val, ok := p.Args[f.Name+"_lt"]; ok {
					conditions = append(conditions, fmt.Sprintf("%s < $%d", f.Name, i))
					values = append(values, val)
					i++
				}
				if val, ok := p.Args[f.Name+"_like"]; ok {
					conditions = append(conditions, fmt.Sprintf("%s ILIKE $%d", f.Name, i))
					values = append(values, "%"+val.(string)+"%")
					i++
				}
			}

			where := ""
			if len(conditions) > 0 {
				where = " WHERE " + strings.Join(conditions, " AND ")
			}

			cols  := selectColumns(fields)
			if isList && len(listFields) > 0 {
				cols = strings.Join(listFields, ", ")
			}
			query := fmt.Sprintf("SELECT %s FROM %s%s", cols, source, where)
			rows, err := db.Query(query, values...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			results, err := scanRows(rows)
			if err != nil {
				return nil, err
			}
			if !isList && len(results) > 0 {
				return results[0], nil
			}
			return results, nil
		},
	}
}

func buildMutationFieldFromAST(ctx dsl.ContextAst, objType *graphql.Object, db *sql.DB) *graphql.Field {
	args := graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.Int}}
	for _, f := range ctx.Fields {
		if f.Readonly || f.Name == "id" {
			continue
		}
		args[f.Name] = &graphql.ArgumentConfig{Type: typeToGraphQL(f.Type)}
	}

	source := ctx.Source
	fields := ctx.Fields

	return &graphql.Field{
		Type: objType,
		Args: args,
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			return executeMutation(db, source, fields, p.Args)
		},
	}
}

func executeMutation(db *sql.DB, source string, fields []dsl.FieldAst, args map[string]interface{}) (interface{}, error) {
	readonlyFields := map[string]bool{}
	for _, f := range fields {
		if f.Readonly {
			readonlyFields[f.Name] = true
		}
	}

	idVal, ok := args["id"]
	if !ok {
		return nil, fmt.Errorf("ID-Feld 'id' fehlt")
	}

	fieldSet := map[string]bool{}
	for _, f := range fields {
		fieldSet[f.Name] = true
	}

	var setClauses []string
	var values    []interface{}
	i := 1

	for key, val := range args {
		if key == "id" || !fieldSet[key] || readonlyFields[key] {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", key, i))
		values = append(values, val)
		i++
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("keine Felder zum Aktualisieren")
	}

	values = append(values, idVal)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = $%d RETURNING %s",
		source, strings.Join(setClauses, ", "), i, selectColumns(fields))

	rows, err := db.Query(query, values...)
	if err != nil {
		return nil, fmt.Errorf("mutation fehlgeschlagen: %w", err)
	}
	defer rows.Close()

	results, err := scanRows(rows)
	if err != nil || len(results) == 0 {
		return nil, err
	}
	return results[0], nil
}

func scanRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var results []map[string]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{})
		for i, col := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = vals[i]
			}
		}
		results = append(results, row)
	}
	return results, nil
}

func selectColumns(fields []dsl.FieldAst) string {
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = f.Name
	}
	return strings.Join(cols, ", ")
}

func typeToGraphQL(t string) *graphql.Scalar {
	switch t {
	case "int":
		return graphql.Int
	case "float":
		return graphql.Float
	default:
		return graphql.String
	}
}

func ContextToFieldName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

func contextToTypeName(name string) string {
	parts := strings.Split(name, "_")
	var b strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

func buildDeleteFieldFromAST(ctx dsl.ContextAst, db *sql.DB) *graphql.Field {
	source := ctx.Source
	return &graphql.Field{
		Type: graphql.Boolean,
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Int)},
		},
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			id := p.Args["id"]
			result, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = $1", source), id)
			if err != nil {
				return false, fmt.Errorf("delete fehlgeschlagen: %w", err)
			}
			rows, _ := result.RowsAffected()
			return rows > 0, nil
		},
	}
}

func buildPluginDeleteField(ctx dsl.ContextAst, caller plugin.Caller) *graphql.Field {
	deleteTool := ctx.Source + "_delete"
	return &graphql.Field{
		Type: graphql.Boolean,
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.Int)},
		},
		Resolve: func(p graphql.ResolveParams) (interface{}, error) {
			id := fmt.Sprintf("%v", p.Args["id"])
			_, err := caller.Call(deleteTool, map[string]string{"id": id})
			if err != nil {
				return false, fmt.Errorf("plugin delete %q: %w", deleteTool, err)
			}
			return true, nil
		},
	}
}

func SelectFields(contextName string) string {
	if activeAST == nil {
		return "id"
	}
	for _, ctx := range activeAST.Contexts {
		if ctx.Name == contextName {
			return selectColumns(ctx.Fields)
		}
	}
	return "id"
}
