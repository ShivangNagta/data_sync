package server

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Object key layout in R2:
// uploads/<file_id>/<version_id>

type R2Client struct {
	client *s3.Client
	bucket string
}

func NewR2Client(endpoint, accessKey, secretKey, bucket string) (*R2Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
		awsconfig.WithBaseEndpoint(endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	svc := s3.NewFromConfig(cfg)
	return &R2Client{client: svc, bucket: bucket}, nil
}

func (r *R2Client) Put(ctx context.Context, fileID, versionID string, data []byte) error {
	key := fmt.Sprintf("uploads/%s/%s", fileID, versionID)
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("r2 put %s: %w", key, err)
	}
	return nil
}

func (r *R2Client) Get(ctx context.Context, fileID, versionID string) ([]byte, error) {
	key := fmt.Sprintf("uploads/%s/%s", fileID, versionID)
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("r2 get %s: %w", key, err)
	}
	defer out.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(out.Body); err != nil {
		return nil, fmt.Errorf("r2 read body %s: %w", key, err)
	}
	return buf.Bytes(), nil
}

func (r *R2Client) Delete(ctx context.Context, fileID, versionID string) error {
	key := fmt.Sprintf("uploads/%s/%s", fileID, versionID)
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("r2 delete %s: %w", key, err)
	}
	return nil
}
