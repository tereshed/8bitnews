// Package profile loads and validates the data-only channel and country
// profiles that drive the 8bitnews pipeline (channels/*/channel.yaml,
// countries/*/sources.yaml). It never reads or writes hub business data.
package profile

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const tbdValue = "tbd"

var errorPrinter = message.NewPrinter(language.English)

// Issue is a single validation finding, formatted for humans as
// "<file>: <field path>: <message>".
type Issue struct {
	File    string
	Path    string
	Message string
}

func (i Issue) String() string {
	p := i.Path
	if p == "" {
		p = "/"
	}
	return fmt.Sprintf("%s: %s: %s", i.File, p, i.Message)
}

// Result is the outcome of validating every profile found under root.
type Result struct {
	Issues []Issue
}

// OK reports whether no issues were found.
func (r Result) OK() bool {
	return len(r.Issues) == 0
}

// Validate loads channels/channel.schema.json and countries/sources.schema.json
// from root, then validates every channels/*/channel.yaml and
// countries/*/sources.yaml against its schema plus the cross-checks described
// in docs/architecture.md §10. When strict is true, any string value equal to
// "tbd" is also reported as an issue.
//
// The returned error is non-nil only for setup failures (schema files
// missing or invalid, directories unreadable) that are not profile content
// problems; those are reported as Issues in Result instead.
func Validate(root string, strict bool) (Result, error) {
	channelSchema, err := compileSchema(filepath.Join(root, "channels", "channel.schema.json"))
	if err != nil {
		return Result{}, fmt.Errorf("loading channel schema: %w", err)
	}
	sourcesSchema, err := compileSchema(filepath.Join(root, "countries", "sources.schema.json"))
	if err != nil {
		return Result{}, fmt.Errorf("loading sources schema: %w", err)
	}

	channelDirs, err := os.ReadDir(filepath.Join(root, "channels"))
	if err != nil {
		return Result{}, fmt.Errorf("reading channels directory: %w", err)
	}

	var result Result

	countryDirs, err := os.ReadDir(filepath.Join(root, "countries"))
	if err != nil {
		return Result{}, fmt.Errorf("reading countries directory: %w", err)
	}
	countryDirNames := make(map[string]bool, len(countryDirs))
	for _, d := range countryDirs {
		if d.IsDir() {
			countryDirNames[d.Name()] = true
		}
	}

	for _, d := range channelDirs {
		if !d.IsDir() {
			continue
		}
		result.Issues = append(result.Issues, validateChannel(root, d.Name(), channelSchema, countryDirNames, strict)...)
	}

	for _, d := range countryDirs {
		if !d.IsDir() {
			continue
		}
		result.Issues = append(result.Issues, validateCountry(root, d.Name(), sourcesSchema, strict)...)
	}

	sort.SliceStable(result.Issues, func(i, j int) bool {
		a, b := result.Issues[i], result.Issues[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Path < b.Path
	})

	return result, nil
}

func compileSchema(path string) (*jsonschema.Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(path, doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c.Compile(path)
}

func validateChannel(root, dirName string, schema *jsonschema.Schema, countryDirNames map[string]bool, strict bool) []Issue {
	relFile := path.Join("channels", dirName, "channel.yaml")
	absFile := filepath.Join(root, "channels", dirName, "channel.yaml")

	data, issues := loadYAML(absFile, relFile)
	if issues != nil {
		return issues
	}

	issues = append(issues, schemaIssues(relFile, schema, data)...)
	if strict {
		issues = append(issues, scanTBD(relFile, data, "")...)
	}

	// Cross-checks only make sense once the shape is roughly right; a
	// missing/mistyped field already produced a schema issue above, so
	// guard every lookup defensively instead of failing fast.
	obj, _ := data.(map[string]any)

	if id, ok := obj["id"].(string); ok && id != dirName {
		issues = append(issues, Issue{
			File:    relFile,
			Path:    "/id",
			Message: fmt.Sprintf("id %q does not match channel directory name %q", id, dirName),
		})
	}

	channelCountry, _ := obj["country"].(string)
	if channelCountry != "" && channelCountry != tbdValue {
		cc := strings.ToLower(channelCountry)
		if !countryDirNames[cc] {
			issues = append(issues, Issue{
				File:    relFile,
				Path:    "/country",
				Message: fmt.Sprintf("no countries/%s directory for country %q", cc, channelCountry),
			})
		}
	}
	channelLanguage, _ := obj["language"].(string)

	if format, ok := obj["format"].(map[string]any); ok {
		if desc, ok := format["description"].(string); ok && desc != tbdValue {
			p := filepath.Join(root, "channels", dirName, desc)
			if !fileExists(p) {
				issues = append(issues, Issue{
					File:    relFile,
					Path:    "/format/description",
					Message: fmt.Sprintf("referenced file %q does not exist", path.Join("channels", dirName, desc)),
				})
			}
		}

		if durationS, ok := format["duration_s"].(map[string]any); ok {
			min, minOK := numericValue(durationS["min"])
			max, maxOK := numericValue(durationS["max"])
			if minOK && maxOK && min > max {
				issues = append(issues, Issue{
					File:    relFile,
					Path:    "/format/duration_s",
					Message: fmt.Sprintf("min (%v) must be <= max (%v)", durationS["min"], durationS["max"]),
				})
			}
		}
	}

	if sources, ok := obj["sources"].(string); ok && sources != tbdValue {
		sourcesAbs := filepath.Join(root, sources)
		if !fileExists(sourcesAbs) {
			issues = append(issues, Issue{
				File:    relFile,
				Path:    "/sources",
				Message: fmt.Sprintf("referenced file %q does not exist", sources),
			})
		} else if sourcesData, sourcesIssues := loadYAML(sourcesAbs, sources); sourcesIssues == nil {
			sourcesObj, _ := sourcesData.(map[string]any)

			if srcCountry, ok := sourcesObj["country"].(string); ok && channelCountry != "" && channelCountry != tbdValue && srcCountry != tbdValue && !strings.EqualFold(channelCountry, srcCountry) {
				issues = append(issues, Issue{
					File:    relFile,
					Path:    "/country",
					Message: fmt.Sprintf("country %q does not match country %q of referenced %s", channelCountry, srcCountry, sources),
				})
			}

			if srcLanguage, ok := sourcesObj["language"].(string); ok && channelLanguage != "" && channelLanguage != tbdValue && srcLanguage != tbdValue && channelLanguage != srcLanguage {
				issues = append(issues, Issue{
					File:    relFile,
					Path:    "/language",
					Message: fmt.Sprintf("language %q does not match language %q of referenced %s", channelLanguage, srcLanguage, sources),
				})
			}
		}
	}

	if policy, ok := obj["policy"].(string); ok && policy != tbdValue {
		if !fileExists(filepath.Join(root, policy)) {
			issues = append(issues, Issue{
				File:    relFile,
				Path:    "/policy",
				Message: fmt.Sprintf("referenced file %q does not exist", policy),
			})
		}
	}

	return issues
}

// numericValue extracts a float64 from the handful of numeric types
// yaml.v3 produces (int, int64, float64) so duration bounds can be compared
// regardless of how the YAML decoder represented them.
func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return float64(n), true
	default:
		return 0, false
	}
}

func validateCountry(root, dirName string, schema *jsonschema.Schema, strict bool) []Issue {
	relFile := path.Join("countries", dirName, "sources.yaml")
	absFile := filepath.Join(root, "countries", dirName, "sources.yaml")

	data, issues := loadYAML(absFile, relFile)
	if issues != nil {
		return issues
	}

	issues = append(issues, schemaIssues(relFile, schema, data)...)
	if strict {
		issues = append(issues, scanTBD(relFile, data, "")...)
	}
	return issues
}

func schemaIssues(relFile string, schema *jsonschema.Schema, data any) []Issue {
	err := schema.Validate(data)
	if err == nil {
		return nil
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []Issue{{File: relFile, Path: "/", Message: err.Error()}}
	}
	var issues []Issue
	collectLeaves(relFile, ve, &issues)
	return issues
}

func collectLeaves(relFile string, ve *jsonschema.ValidationError, issues *[]Issue) {
	if len(ve.Causes) == 0 {
		*issues = append(*issues, Issue{
			File:    relFile,
			Path:    jsonPointer(ve.InstanceLocation),
			Message: ve.ErrorKind.LocalizedString(errorPrinter),
		})
		return
	}
	for _, cause := range ve.Causes {
		collectLeaves(relFile, cause, issues)
	}
}

func jsonPointer(tokens []string) string {
	if len(tokens) == 0 {
		return "/"
	}
	return "/" + strings.Join(tokens, "/")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// scanTBD recursively reports every string value equal to "tbd" below data,
// using JSON-pointer-style paths rooted at basePath.
func scanTBD(relFile string, data any, basePath string) []Issue {
	var issues []Issue
	switch v := data.(type) {
	case string:
		if v == tbdValue {
			issues = append(issues, Issue{
				File:    relFile,
				Path:    jsonPointerStr(basePath),
				Message: `value "tbd" is not allowed in --strict mode`,
			})
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			issues = append(issues, scanTBD(relFile, v[k], basePath+"/"+k)...)
		}
	case []any:
		for i, item := range v {
			issues = append(issues, scanTBD(relFile, item, fmt.Sprintf("%s/%d", basePath, i))...)
		}
	}
	return issues
}

func jsonPointerStr(p string) string {
	if p == "" {
		return "/"
	}
	return p
}
