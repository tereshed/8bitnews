// Package storage talks to the S3-compatible object store (MinIO locally,
// any S3-compatible provider in production) that holds release media:
// presigned uploads for media_upload_url and existence checks for
// release_submit.
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/tereshed/8bitnews/internal/config"
)

// Client wraps an S3 client and its presigner for one bucket.
type Client struct {
	api           *s3.Client
	presign       *s3.PresignClient
	bucket        string
	presignExpiry time.Duration
}

// New builds a Client from the given S3 settings.
func New(ctx context.Context, cfg config.S3Config) (*Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("loading S3 config: %w", err)
	}

	api := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})

	return &Client{
		api:           api,
		presign:       s3.NewPresignClient(api),
		bucket:        cfg.Bucket,
		presignExpiry: cfg.PresignExpiry,
	}, nil
}

// PresignPut returns a presigned PUT URL for key, valid for the configured
// expiry, used by media_upload_url.
func (c *Client) PresignPut(ctx context.Context, key, contentType string) (string, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(c.presignExpiry))
	if err != nil {
		return "", fmt.Errorf("presigning PUT for %s: %w", key, err)
	}
	return req.URL, nil
}

// Exists reports whether key is present in the bucket, used by
// release_submit to verify every object key in the manifest before
// accepting the release.
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == 404 {
		return false, nil
	}
	return false, fmt.Errorf("checking object %s: %w", key, err)
}
