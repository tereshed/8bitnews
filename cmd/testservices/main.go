// Command testservices initializes the isolated integration-test bucket.
package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	if os.Getenv("HUB_TEST_REQUIRE_SERVICES") != "true" {
		log.Fatal("run scripts/test-integration.sh")
	}
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(os.Getenv("HUB_TEST_S3_ACCESS_KEY"), os.Getenv("HUB_TEST_S3_SECRET_KEY"), "")))
	if err != nil {
		log.Fatal(err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(os.Getenv("HUB_TEST_S3_ENDPOINT"))
		o.UsePathStyle = true
	})
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(os.Getenv("HUB_TEST_S3_BUCKET"))}); err != nil {
		log.Fatal(err)
	}
}
