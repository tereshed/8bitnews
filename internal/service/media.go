package service

import (
	"context"
	"errors"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/tereshed/8bitnews/internal/store"
)

var unsafeObjectKeyChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// MediaUploadURL returns a presigned PUT URL and the S3 object key media
// should be uploaded to (task E1 §3).
func (s *Service) MediaUploadURL(ctx context.Context, req MediaUploadURLRequest) (MediaUploadURLResponse, error) {
	if req.DevelopmentID == "" {
		return MediaUploadURLResponse{}, invalidf("development_id is required")
	}
	if req.Filename == "" {
		return MediaUploadURLResponse{}, invalidf("filename is required")
	}
	if req.ContentType == "" {
		return MediaUploadURLResponse{}, invalidf("content_type is required")
	}

	if _, err := s.store.GetDevelopment(ctx, req.DevelopmentID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return MediaUploadURLResponse{}, notFoundf("development %q not found", req.DevelopmentID)
		}
		return MediaUploadURLResponse{}, err
	}

	key := objectKey(req.DevelopmentID, req.Filename)
	uploadURL, err := s.media.PresignPut(ctx, key, req.ContentType)
	if err != nil {
		return MediaUploadURLResponse{}, err
	}
	return MediaUploadURLResponse{UploadURL: uploadURL, ObjectKey: key}, nil
}

// objectKey builds a collision-resistant S3 key for a development's media
// upload, keeping the original extension so content type sniffing by
// downstream tools keeps working.
func objectKey(developmentID, filename string) string {
	ext := path.Ext(filename)
	base := strings.TrimSuffix(path.Base(filename), ext)
	base = unsafeObjectKeyChars.ReplaceAllString(base, "-")
	if base == "" {
		base = "file"
	}
	return "releases/" + developmentID + "/" + uuid.NewString() + "-" + base + ext
}
