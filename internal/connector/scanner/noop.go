package scanner

import (
	"context"
	"io"
)

// Noop marks every file CLEAN without inspecting it. DEVELOPMENT ONLY: it
// exists so the upload -> scan -> available lifecycle can be exercised before a
// real engine is connected. Never use it in production.
type Noop struct{}

func (Noop) Name() string { return "noop" }

func (Noop) Scan(ctx context.Context, r io.Reader) (Result, error) {
	// Drain the reader so storage read paths are exercised end to end.
	if _, err := io.Copy(io.Discard, &ctxReader{ctx: ctx, r: r}); err != nil {
		return Result{}, err
	}
	return Result{Clean: true}, nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
