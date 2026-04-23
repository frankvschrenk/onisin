package tools

// status.go — System status query tool.

import (
	"fmt"
	"strings"

	"onisin.com/oos/helper"
)

// Status returns a human-readable summary of the current OOS system state,
// including the active context and available GraphQL queries.
func Status() string {
	ctx := helper.Stage.CurrentContext
	if ctx == "" {
		ctx = "(none)"
	}

	var schema strings.Builder
	if helper.OOSAst != nil {
		for _, c := range helper.OOSAst.Contexts {
			dsnName := c.DSN
			if dsnName == "" {
				dsnName = c.Source
			}
			if _, ok := helper.DsnRegistry[dsnName]; !ok {
				continue
			}
			fields := make([]string, 0, len(c.Fields))
			for _, f := range c.Fields {
				fields = append(fields, f.Name)
			}
			schema.WriteString(fmt.Sprintf("\n  %s { %s }", c.GQLQuery, strings.Join(fields, ", ")))
		}
	}

	return fmt.Sprintf("status: online | active context: %s\navailable queries:%s",
		ctx, schema.String())
}
