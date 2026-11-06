package storage

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Options configures the S3-compatible connector (AWS S3, MinIO, ...).
type S3Options struct {
	Endpoint     string // host[:port], without scheme
	Bucket       string
	AccessKey    string
	SecretKey    string
	Region       string
	UseSSL       bool
	CreateBucket bool
}

// S3 stores objects in a private bucket. Objects are never exposed through
// presigned URLs; all reads go through the DMS so scan status is enforced.
type S3 struct {
	client *minio.Client
	bucket string
}

func NewS3(ctx context.Context, o S3Options) (*S3, error) {
	client, err := minio.New(o.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(o.AccessKey, o.SecretKey, ""),
		Secure: o.UseSSL,
		Region: o.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create s3 client: %w", err)
	}

	exists, err := client.BucketExists(ctx, o.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: check bucket %q: %w", o.Bucket, err)
	}
	if !exists {
		if !o.CreateBucket {
			return nil, fmt.Errorf("storage: bucket %q does not exist (set S3_CREATE_BUCKET=true to create it)", o.Bucket)
		}
		if err := client.MakeBucket(ctx, o.Bucket, minio.MakeBucketOptions{Region: o.Region}); err != nil {
			return nil, fmt.Errorf("storage: create bucket %q: %w", o.Bucket, err)
		}
	}
	return &S3{client: client, bucket: o.Bucket}, nil
}

func (s *S3) Name() string { return "s3" }

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("storage: put %q: %w", key, err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %q: %w", key, err)
	}
	// GetObject is lazy; Stat surfaces "not found" before we start streaming.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: stat %q: %w", key, err)
	}
	return obj, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: delete %q: %w", key, err)
	}
	return nil
}
