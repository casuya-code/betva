// Command r2bucket creates an R2 bucket for the backup mirror using the
// account's S3 credentials. Run locally with R2_ACCOUNT_ID / R2_ACCESS_KEY_ID /
// R2_SECRET_ACCESS_KEY / R2_BUCKET set; it does not need a Cloudflare API token.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	account := os.Getenv("R2_ACCOUNT_ID")
	bucket := os.Getenv("R2_BUCKET")
	if account == "" || bucket == "" || os.Getenv("R2_ACCESS_KEY_ID") == "" || os.Getenv("R2_SECRET_ACCESS_KEY") == "" {
		log.Fatal("r2bucket: set R2_ACCOUNT_ID, R2_BUCKET, R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY")
	}

	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			os.Getenv("R2_ACCESS_KEY_ID"), os.Getenv("R2_SECRET_ACCESS_KEY"), "",
		)),
	)
	if err != nil {
		log.Fatal(err)
	}
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", account)
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = false
	})

	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		log.Fatalf("r2bucket: create %s: %v", bucket, err)
	}
	fmt.Printf("created bucket %s at %s\n", bucket, endpoint)
}
