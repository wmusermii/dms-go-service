package storage

import (
	"context"
	"fmt"

	"dms/internal/config"
)

// New builds the storage backend selected by STORAGE_DRIVER.
// To add a backend: implement Storage, then register it here.
func New(ctx context.Context, cfg config.StorageConfig) (Storage, error) {
	switch cfg.Driver {
	case "local":
		return NewLocal(cfg.LocalPath)
	case "s3":
		return NewS3(ctx, S3Options{
			Endpoint:     cfg.S3Endpoint,
			Bucket:       cfg.S3Bucket,
			AccessKey:    cfg.S3AccessKey,
			SecretKey:    cfg.S3SecretKey,
			Region:       cfg.S3Region,
			UseSSL:       cfg.S3UseSSL,
			CreateBucket: cfg.S3CreateBucket,
		})
	default:
		return nil, fmt.Errorf("unknown storage driver %q", cfg.Driver)
	}
}
