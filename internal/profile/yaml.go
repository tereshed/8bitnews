package profile

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// loadYAML reads and decodes a YAML file into JSON-compatible Go values
// (map[string]any, []any, string, bool, int, float64, nil) so it can be fed
// directly to jsonschema.Schema.Validate. On any I/O or syntax error it
// returns a single Issue instead of an error, so callers can keep validating
// the remaining profiles.
func loadYAML(absFile, relFile string) (data any, issues []Issue) {
	raw, err := os.ReadFile(absFile)
	if err != nil {
		return nil, []Issue{{File: relFile, Path: "/", Message: fmt.Sprintf("cannot read file: %s", err)}}
	}

	var decoded any
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		return nil, []Issue{{File: relFile, Path: "/", Message: fmt.Sprintf("invalid YAML: %s", err)}}
	}

	return normalize(decoded), nil
}

// normalize converts values produced by yaml.v3 into the subset of Go types
// that encoding/json would produce, which is what jsonschema.Schema.Validate
// and our own recursive helpers expect.
func normalize(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = normalize(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[fmt.Sprintf("%v", k)] = normalize(item)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = normalize(item)
		}
		return out
	default:
		return val
	}
}
