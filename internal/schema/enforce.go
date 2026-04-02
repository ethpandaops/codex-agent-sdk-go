// Package schema provides JSON Schema normalization helpers for the
// Codex Agent SDK.
package schema

// EnforceAdditionalProperties recursively sets
// "additionalProperties": false on every "type": "object" node that
// does not already specify the field. OpenAI's structured-output API
// requires this on all object nodes, not just the root.
func EnforceAdditionalProperties(m map[string]any) {
	typ, _ := m["type"].(string)
	if typ == "object" {
		if _, has := m["additionalProperties"]; !has {
			m["additionalProperties"] = false
		}
	}

	enforceInMapValues(m, "properties")
	enforceInMapValues(m, "$defs")

	if items, ok := m["items"].(map[string]any); ok {
		EnforceAdditionalProperties(items)
	}

	enforceInSliceValues(m, "anyOf")
}

// enforceInMapValues applies EnforceAdditionalProperties to each map
// value under the given key.
func enforceInMapValues(m map[string]any, key string) {
	container, ok := m[key].(map[string]any)
	if !ok {
		return
	}

	for _, v := range container {
		if sub, isMap := v.(map[string]any); isMap {
			EnforceAdditionalProperties(sub)
		}
	}
}

// enforceInSliceValues applies EnforceAdditionalProperties to each
// map element in a slice under the given key.
func enforceInSliceValues(m map[string]any, key string) {
	arr, ok := m[key].([]any)
	if !ok {
		return
	}

	for _, item := range arr {
		if sub, isMap := item.(map[string]any); isMap {
			EnforceAdditionalProperties(sub)
		}
	}
}
