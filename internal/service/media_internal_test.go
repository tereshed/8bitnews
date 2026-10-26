package service

import (
	"strings"
	"testing"
)

func TestObjectKey(t *testing.T) {
	key := objectKey("dev-123", "My Video (final)!!.mp4")
	if !strings.HasPrefix(key, "releases/dev-123/") {
		t.Fatalf("objectKey = %q, want prefix releases/dev-123/", key)
	}
	if !strings.HasSuffix(key, ".mp4") {
		t.Fatalf("objectKey = %q, want suffix .mp4", key)
	}
	if strings.ContainsAny(key, " !()") {
		t.Fatalf("objectKey = %q, must not contain unsafe characters", key)
	}
}

func TestObjectKey_Unique(t *testing.T) {
	a := objectKey("dev-123", "same-name.mp4")
	b := objectKey("dev-123", "same-name.mp4")
	if a == b {
		t.Fatalf("objectKey must be unique per call, got %q twice", a)
	}
}
