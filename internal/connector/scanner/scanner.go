// Package scanner is the connector layer for malware scanning. Only the
// interface and a development stub ship with the DMS; plug a real engine
// (ClamAV, a cloud scanning API, ...) in by implementing Scanner and
// registering it in factory.go.
package scanner

import (
	"context"
	"io"
)

// Result is the outcome of scanning one file.
type Result struct {
	Clean     bool   // true when no threat was found
	Signature string // detected threat name, when Clean is false
}

type Scanner interface {
	// Scan reads r to the end and reports whether the content is clean.
	// A returned error means the scan itself failed (engine down, timeout);
	// the worker retries those. A detected threat is NOT an error.
	Scan(ctx context.Context, r io.Reader) (Result, error)

	// Name identifies the engine for logs and audit entries.
	Name() string
}
