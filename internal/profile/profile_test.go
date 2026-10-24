package profile_test

import (
	"strings"
	"testing"

	"github.com/tereshed/8bitnews/internal/profile"
)

func TestValidate_ValidProfile(t *testing.T) {
	result, err := profile.Validate("testdata/valid", false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no issues, got: %v", result.Issues)
	}
}

func TestValidate_ValidProfile_StrictHasNoTBD(t *testing.T) {
	result, err := profile.Validate("testdata/valid", true)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no issues in strict mode, got: %v", result.Issues)
	}
}

func TestValidate_MissingRequiredField(t *testing.T) {
	result, err := profile.Validate("testdata/missing_field", false)
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
	result, err := profile.Validate("testdata/broken_ref", false)
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
	result, err := profile.Validate("testdata/tbd_strict", false)
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected no issues in non-strict mode, got: %v", result.Issues)
	}

	result, err = profile.Validate("testdata/tbd_strict", true)
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
