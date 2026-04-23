package boot

// dsl_fetch.go — DSL screen definition fetching from OOSP.

import (
	"encoding/json"
	"fmt"

	"onisin.com/oos-dsl/dsl"
	"onisin.com/oos/helper"
)

// fetchDSLEnvelope fetches a rendered DSL screen envelope from OOSP for the
// given context name and optional content data.
func fetchDSLEnvelope(contextName string, content map[string]any) (map[string]any, error) {
	if helper.OOSP == nil {
		return nil, fmt.Errorf("OOSP not connected")
	}

	contentStr := ""
	if len(content) > 0 {
		b, err := json.Marshal(content)
		if err == nil {
			contentStr = string(b)
		}
	}

	raw, err := helper.OOSP.Call("oosp_dsl", map[string]string{
		"id":      contextName,
		"content": contentStr,
	})
	if err != nil {
		return nil, fmt.Errorf("oosp_dsl: %w", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil, fmt.Errorf("envelope parse: %w", err)
	}

	return envelope, nil
}

// deserializeNode unmarshals a JSON byte slice into a DSL Node tree.
func deserializeNode(data []byte) (*dsl.Node, error) {
	var node dsl.Node
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("node unmarshal: %w", err)
	}
	if node.Type == "" {
		return nil, fmt.Errorf("node has no type — not a valid DSL node tree")
	}
	return &node, nil
}
