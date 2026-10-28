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

// downloadablePrefixes are the parts of the media bucket agents may read:
// release media (QA and Legal look at a video's keyframes) and the music
// catalog's audio (the Video team mixes a track in). Backups live in
// another bucket altogether.
var downloadablePrefixes = []string{"releases/", "music/"}

// MediaDownloadURL returns a presigned GET URL for an existing object under
// one of downloadablePrefixes.
func (s *Service) MediaDownloadURL(ctx context.Context, req MediaDownloadURLRequest) (MediaDownloadURLResponse, error) {
	key := req.ObjectKey
	if key == "" {
		return MediaDownloadURLResponse{}, invalidf("object_key is required")
	}
	if err := checkDownloadKey(key); err != nil {
		return MediaDownloadURLResponse{}, err
	}
	ok, err := s.media.Exists(ctx, key)
	if err != nil {
		return MediaDownloadURLResponse{}, err
	}
	if !ok {
		return MediaDownloadURLResponse{}, notFoundf("object %q not found", key)
	}
	url, err := s.media.PresignGet(ctx, key)
	if err != nil {
		return MediaDownloadURLResponse{}, err
	}
	return MediaDownloadURLResponse{DownloadURL: url, ObjectKey: key}, nil
}

func checkDownloadKey(key string) error {
	allowed := false
	for _, p := range downloadablePrefixes {
		if strings.HasPrefix(key, p) {
			allowed = true
			break
		}
	}
	if !allowed {
		return invalidf("object_key %q is outside %s", key, strings.Join(downloadablePrefixes, ", "))
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return invalidf("object_key %q has an empty or relative path segment", key)
		}
	}
	return nil
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
