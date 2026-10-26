// Package profilesync implements `hub sync-profiles`: it validates the
// data-only channel and country profiles with internal/profile, then
// mirrors them into Postgres so the API layer never has to read the
// repository checkout at request time.
package profilesync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tereshed/8bitnews/internal/profile"
	"github.com/tereshed/8bitnews/internal/store"
)

// Result reports how many profiles were synced.
type Result struct {
	Countries int
	Channels  int
}

// Sync validates every profile under repoRoot (channels/, countries/) and
// upserts it into the store. It fails loud: an invalid profile, or any file
// it cannot read, aborts the whole sync rather than partially applying it.
func Sync(ctx context.Context, st *store.Store, repoRoot string) (Result, error) {
	validation, err := profile.Validate(repoRoot, false)
	if err != nil {
		return Result{}, fmt.Errorf("validating profiles: %w", err)
	}
	if !validation.OK() {
		var msgs []string
		for _, issue := range validation.Issues {
			msgs = append(msgs, issue.String())
		}
		return Result{}, fmt.Errorf("profiles failed validation:\n%s", strings.Join(msgs, "\n"))
	}

	var res Result

	countryDirs, err := os.ReadDir(filepath.Join(repoRoot, "countries"))
	if err != nil {
		return Result{}, fmt.Errorf("reading countries directory: %w", err)
	}
	for _, d := range countryDirs {
		if !d.IsDir() {
			continue
		}
		if err := syncCountry(ctx, st, repoRoot, d.Name()); err != nil {
			return Result{}, err
		}
		res.Countries++
	}

	channelDirs, err := os.ReadDir(filepath.Join(repoRoot, "channels"))
	if err != nil {
		return Result{}, fmt.Errorf("reading channels directory: %w", err)
	}
	for _, d := range channelDirs {
		if !d.IsDir() {
			continue
		}
		if err := syncChannel(ctx, st, repoRoot, d.Name()); err != nil {
			return Result{}, err
		}
		res.Channels++
	}

	return res, nil
}

func syncCountry(ctx context.Context, st *store.Store, repoRoot, cc string) error {
	dir := filepath.Join(repoRoot, "countries", cc)

	data, err := loadYAMLAsJSON(filepath.Join(dir, "sources.yaml"))
	if err != nil {
		return fmt.Errorf("loading countries/%s/sources.yaml: %w", cc, err)
	}
	var fields struct {
		Language string `json:"language"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decoding countries/%s/sources.yaml: %w", cc, err)
	}

	policyMD, err := readFileIfExists(filepath.Join(dir, "policy.md"))
	if err != nil {
		return err
	}
	legalMD, err := readFileIfExists(filepath.Join(dir, "legal.md"))
	if err != nil {
		return err
	}

	return st.UpsertCountry(ctx, store.Country{
		CC:       cc,
		Name:     strings.ToUpper(cc),
		Language: fields.Language,
		PolicyMD: policyMD,
		LegalMD:  legalMD,
		Data:     data,
	})
}

func syncChannel(ctx context.Context, st *store.Store, repoRoot, id string) error {
	dir := filepath.Join(repoRoot, "channels", id)

	data, err := loadYAMLAsJSON(filepath.Join(dir, "channel.yaml"))
	if err != nil {
		return fmt.Errorf("loading channels/%s/channel.yaml: %w", id, err)
	}
	var fields struct {
		ID        string          `json:"id"`
		Country   string          `json:"country"`
		Name      string          `json:"name"`
		Language  string          `json:"language"`
		Timezone  string          `json:"timezone"`
		Platforms json.RawMessage `json:"platforms"`
		Format    json.RawMessage `json:"format"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decoding channels/%s/channel.yaml: %w", id, err)
	}

	formatMD, err := readFileIfExists(filepath.Join(dir, "format.md"))
	if err != nil {
		return err
	}
	metadataMD, err := readFileIfExists(filepath.Join(dir, "metadata.md"))
	if err != nil {
		return err
	}

	return st.UpsertChannel(ctx, store.Channel{
		ID:         id,
		CountryCC:  strings.ToLower(fields.Country),
		Name:       fields.Name,
		Language:   fields.Language,
		Timezone:   fields.Timezone,
		Platforms:  fields.Platforms,
		Format:     fields.Format,
		FormatMD:   formatMD,
		MetadataMD: metadataMD,
		Data:       data,
	})
}

// loadYAMLAsJSON reads a YAML profile file and re-encodes it as JSON, so the
// store can hold it in a jsonb column; gopkg.in/yaml.v3 already decodes
// mappings into map[string]any when the target is `any`, so no extra
// normalization is needed before json.Marshal.
func loadYAMLAsJSON(path string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("encoding as JSON: %w", err)
	}
	return encoded, nil
}

func readFileIfExists(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(raw), nil
}
