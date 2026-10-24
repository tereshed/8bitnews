package profile_test

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tereshed/8bitnews/internal/profile"
)

// repoSchema points at the real, single-source-of-truth schema files under
// the repository root. Fixtures must validate against these, not against
// stale copies, or a schema edit could silently stop being exercised by the
// tests.
const (
	repoChannelSchema = "../../channels/channel.schema.json"
	repoSourcesSchema = "../../countries/sources.schema.json"
)

// fixtureRoot assembles a self-contained profile root in t.TempDir(): the
// data-only files from testdata/<name>, plus the real schemas copied in from
// the repository root. profile.Validate reads channels/channel.schema.json
// and countries/sources.schema.json from the root it is given, so this is
// the only way to exercise the fixtures against the schemas that actually
// ship.
func fixtureRoot(t *testing.T, name string) string {
	t.Helper()

	src := filepath.Join("testdata", name)
	root := t.TempDir()

	copyTree(t, filepath.Join(src, "channels"), filepath.Join(root, "channels"))
	copyTree(t, filepath.Join(src, "countries"), filepath.Join(root, "countries"))
	copyFile(t, repoChannelSchema, filepath.Join(root, "channels", "channel.schema.json"))
	copyFile(t, repoSourcesSchema, filepath.Join(root, "countries", "sources.schema.json"))

	return root
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		copyFile(t, path, target)
		return nil
	})
	if err != nil {
		t.Fatalf("copying %s: %v", src, err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(dst), err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("opening %s: %v", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("creating %s: %v", dst, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copying %s to %s: %v", src, dst, err)
	}
}

func TestValidate_ValidProfile(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "valid"), false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no issues, got: %v", result.Issues)
	}
}

func TestValidate_ValidProfile_StrictHasNoTBD(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "valid"), true)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no issues in strict mode, got: %v", result.Issues)
	}
}

func TestValidate_MissingRequiredField(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "missing_field"), false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected issues for missing field, got none")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.File == "channels/demo-us/channel.yaml" && strings.Contains(issue.Message, "language") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an issue mentioning channel.yaml and 'language', got: %v", result.Issues)
	}
}

func TestValidate_NonExistentReferencedFile(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "broken_ref"), false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected issues for a broken file reference, got none")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Path == "/sources" && strings.Contains(issue.Message, "does not exist") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an issue about the missing referenced file, got: %v", result.Issues)
	}
}

func TestValidate_StrictRejectsTBD(t *testing.T) {
	root := fixtureRoot(t, "tbd_strict")

	result, err := profile.Validate(root, false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no issues in non-strict mode, got: %v", result.Issues)
	}

	result, err = profile.Validate(root, true)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected a tbd issue in strict mode, got none")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Path == "/platforms/youtube/account" && strings.Contains(issue.Message, "tbd") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an issue about the tbd placeholder, got: %v", result.Issues)
	}
}

func TestValidate_InvalidLeaning(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "invalid_leaning"), false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected issues for an invalid leaning value, got none")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.File == "countries/us/sources.yaml" && strings.Contains(issue.Path, "leaning") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an issue about the invalid leaning value, got: %v", result.Issues)
	}
}

func TestValidate_DurationMinGreaterThanMax(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "bad_duration"), false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected an issue for min > max duration, got none")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.File == "channels/demo-us/channel.yaml" && issue.Path == "/format/duration_s" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an issue about duration_s min/max, got: %v", result.Issues)
	}
}

func TestValidate_UnknownPlatform(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "unknown_platform"), false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected an issue for an unknown platform key, got none")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.File == "channels/demo-us/channel.yaml" && strings.Contains(issue.Message, "youtube") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an issue about the unknown platform key, got: %v", result.Issues)
	}
}

func TestValidate_ChannelCountryMismatchesSources(t *testing.T) {
	result, err := profile.Validate(fixtureRoot(t, "country_mismatch"), false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected an issue for a channel/sources country mismatch, got none")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.File == "channels/demo-us/channel.yaml" && issue.Path == "/country" && strings.Contains(issue.Message, "does not match") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an issue about the country mismatch, got: %v", result.Issues)
	}
}
