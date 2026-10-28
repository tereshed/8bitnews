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

func TestCheckDownloadKey(t *testing.T) {
	for _, ok := range []string{"releases/dev-1/abc-video.mp4", "music/tracks/oga/x.ogg"} {
		if err := checkDownloadKey(ok); err != nil {
			t.Errorf("checkDownloadKey(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"backups/db.dump", "releases/../backups/db.dump", "music//x.ogg", "release/x", "/releases/x", "releases/./x"} {
		if err := checkDownloadKey(bad); err == nil {
			t.Errorf("checkDownloadKey(%q) = nil, want an error", bad)
		}
	}
}
