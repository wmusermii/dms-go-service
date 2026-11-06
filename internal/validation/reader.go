package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
)

// ErrTooLarge is returned by LimitedReader once more than max bytes were read.
var ErrTooLarge = errors.New("file exceeds the maximum allowed size")

// LimitedReader fails (instead of silently truncating, like io.LimitReader)
// when the stream is larger than max, so oversized uploads are rejected.
type LimitedReader struct {
	r        io.Reader
	max, n   int64
	exceeded bool
}

func NewLimitedReader(r io.Reader, max int64) *LimitedReader {
	return &LimitedReader{r: r, max: max}
}

func (l *LimitedReader) Read(p []byte) (int, error) {
	if l.exceeded {
		return 0, ErrTooLarge
	}
	n, err := l.r.Read(p)
	l.n += int64(n)
	if l.n > l.max {
		l.exceeded = true
		return 0, ErrTooLarge
	}
	return n, err
}

// Exceeded reports whether the size limit was hit.
func (l *LimitedReader) Exceeded() bool { return l.exceeded }

// HashReader computes the SHA-256 and the byte count while the stream is
// consumed, so large files are never buffered in memory.
type HashReader struct {
	r io.Reader
	h hash.Hash
	n int64
}

func NewHashReader(r io.Reader) *HashReader {
	return &HashReader{r: r, h: sha256.New()}
}

func (h *HashReader) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	if n > 0 {
		h.h.Write(p[:n])
		h.n += int64(n)
	}
	return n, err
}

// Sum returns the lower-case hex SHA-256 of everything read so far.
func (h *HashReader) Sum() string { return hex.EncodeToString(h.h.Sum(nil)) }

// Size returns the number of bytes read so far.
func (h *HashReader) Size() int64 { return h.n }
