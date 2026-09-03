package server

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Object key layout in R2:
// uploads/<file_id>/<version_id>

type R2Client struct {
	client *s3.Client
	bucket string
}

func NewR2Client(endpoint, accessKey, secretKey, bucket string) (*R2Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("auto"),
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

// Clear removes every object in the bucket. R2 has no single "empty bucket"
// call, so we paginate ListObjectsV2 and batch-delete (max 1000 keys per
// request). Safe to call on an already-empty bucket.
func (r *R2Client) Clear(ctx context.Context) error {
	var keys []string
	collect := func(objs []types.Object) {
		for _, o := range objs {
			if o.Key != nil {
				keys = append(keys, *o.Key)
			}
		}
	}

	// Page through the bucket collecting object keys.
	paginator := s3.NewListObjectsV2Paginator(r.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(r.bucket),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("r2 list objects: %w", err)
		}
		collect(page.Contents)
	}

	// Batch-delete in chunks of 1000 (S3 API limit).
	const batch = 1000
	for i := 0; i < len(keys); i += batch {
		end := i + batch
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[i:end]
		del := make([]types.ObjectIdentifier, len(chunk))
		for j, k := range chunk {
			del[j] = types.ObjectIdentifier{Key: aws.String(k)}
		}
		out, err := r.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(r.bucket),
			Delete: &types.Delete{Objects: del},
		})
		if err != nil {
			return fmt.Errorf("r2 delete batch: %w", err)
		}
		// R2 returns errors per object in the response body; surface them.
		if len(out.Errors) > 0 {
			return fmt.Errorf("r2 delete batch had %d errors (e.g. %s: %s)",
				len(out.Errors), strValue(out.Errors[0].Key), strValue(out.Errors[0].Message))
		}
	}
	return nil
}

func strValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
