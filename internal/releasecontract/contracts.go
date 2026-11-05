// Package releasecontract validates content artifacts at the hub trust boundary.
package releasecontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tereshed/8bitnews/newsroom/contracts"
)

var load = sync.OnceValues(func() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	kinds := []string{"research_dossier", "script", "video_package", "validation_report", "legal_review"}
	for _, kind := range kinds {
		name := kind + ".schema.json"
		data, err := contracts.FS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if err := compiler.AddResource("https://8bitnews.local/contracts/"+name, doc); err != nil {
			return nil, err
		}
	}
	result := map[string]*jsonschema.Schema{}
	for _, kind := range kinds {
		schema, err := compiler.Compile("https://8bitnews.local/contracts/" + kind + ".schema.json")
		if err != nil {
			return nil, err
		}
		result[kind] = schema
	}
	return result, nil
})

func Validate(kind string, raw json.RawMessage) error {
	schemas, err := load()
	if err != nil {
		return fmt.Errorf("loading artifact contracts: %w", err)
	}
	schema, ok := schemas[kind]
	if !ok {
		return fmt.Errorf("unknown artifact kind %q", kind)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	if err := schema.Validate(doc); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	return nil
}
