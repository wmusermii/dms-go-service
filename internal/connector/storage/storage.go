// Package storage is the connector layer for physical file storage. Services
// only depend on the Storage interface, so backends can be swapped by config.
package storage

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned by Get when the object does not exist.
var ErrNotFound = errors.New("storage: object not found")

type Storage interface {
	// Put streams r into the object at key. size is the exact byte length or
	// -1 when unknown. Implementations must not leave partial objects behind
	// when r fails.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	// Get opens the object for reading. The caller must Close the result.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the object. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error

	// Name identifies the driver for logs.
	Name() string
}
