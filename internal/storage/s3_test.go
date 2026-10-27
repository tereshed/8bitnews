package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/config"
)

// TestPresignPut_CloudflareR2 guards the E1d deploy's R2 config (region
// "auto", a *.r2.cloudflarestorage.com endpoint, path-style addressing)
// against a regression in the AWS SDK's checksum defaults: aws-sdk-go-v2
// defaults non-presigned S3 writes to compute a trailing checksum
// (RequestChecksumCalculationWhenSupported), which Cloudflare R2 largely
// doesn't implement. PresignPutObject already pins checksum calculation to
// "when required" internally (see withNoDefaultChecksumAPIOption in
// service/s3), so a correctly built presigned URL must carry no
// checksum-related query parameter — this test fails if a future SDK
// upgrade changes that, or breaks path-style/region="auto" presigning.
func TestPresignPut_CloudflareR2(t *testing.T) {
	cfg := config.S3Config{
		Endpoint:        "https://abc123.r2.cloudflarestorage.com",
		Region:          "auto",
		Bucket:          "8bitnews-media",
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
		UsePathStyle:    true,
		PresignExpiry:   15 * time.Minute,
	}

	client, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rawURL, err := client.PresignPut(context.Background(), "media/release-1/cover.png", "image/png")
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parsing presigned URL: %v", err)
	}
	if u.Host != "abc123.r2.cloudflarestorage.com" {
		t.Errorf("host = %q, want the R2 endpoint host", u.Host)
	}
	if !strings.HasPrefix(u.Path, "/8bitnews-media/") {
		t.Errorf("path = %q, want a path-style bucket prefix", u.Path)
	}
	credential, err := url.QueryUnescape(u.Query().Get("X-Amz-Credential"))
	if err != nil {
		t.Fatalf("unescaping X-Amz-Credential: %v", err)
	}
	if !strings.Contains(credential, "/auto/s3/aws4_request") {
		t.Errorf("credential scope = %q, want it signed for region %q", credential, cfg.Region)
	}
	for _, forbidden := range []string{"checksum"} {
		if strings.Contains(strings.ToLower(u.RawQuery), forbidden) {
			t.Errorf("query = %q contains %q; R2 rejects presigned PUTs carrying checksum parameters", u.RawQuery, forbidden)
		}
	}
}
