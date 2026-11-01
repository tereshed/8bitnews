package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
)

// An id that isn't a uuid is the caller's mistake: it must come back as an
// invalid-argument error (HTTP 400, an MCP tool error), never reach Postgres.
// These calls fail before any store call, so they run without a database.
func TestIDsThatAreNotUUIDsAreInvalid(t *testing.T) {
	svc := service.New(service.Deps{})
	ctx := context.Background()
	bad := "story-123"
	calls := map[string]func() error{
		"story_register": func() error {
			_, err := svc.StoryRegister(ctx, service.StoryRegisterRequest{
				ChannelID: "c", StoryID: &bad, DevelopmentSummary: "x", ReportedAt: time.Now()})
			return err
		},
		"experiment_assign": func() error {
			_, err := svc.ExperimentAssign(ctx, service.ExperimentAssignRequest{ChannelID: "c", DevelopmentID: "null"})
			return err
		},
		"media_upload_url": func() error {
			_, err := svc.MediaUploadURL(ctx, service.MediaUploadURLRequest{DevelopmentID: "dev-1", Filename: "v.mp4", ContentType: "video/mp4"})
			return err
		},
		"release_submit": func() error {
			_, err := svc.ReleaseSubmit(ctx, service.ReleaseManifest{DevelopmentID: "dev-1", ChannelID: "c"})
			return err
		},
		"release_get": func() error {
			_, err := svc.ReleaseGet(ctx, service.ReleaseGetRequest{ReleaseID: "7f01440f"})
			return err
		},
	}
	for name, call := range calls {
		var svcErr *service.Error
		if err := call(); !errors.As(err, &svcErr) || svcErr.Kind != service.KindInvalid {
			t.Errorf("%s: %v; want an invalid-argument error", name, err)
		}
	}
}
