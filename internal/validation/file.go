// Package validation holds the upload checks: size, extension, content-based
// MIME detection, filename sanitising and checksum helpers.
package validation

import (
	"fmt"
	"mime"
	"net/http"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"dms/internal/domain"
)

// typeRule links a file extension to the content types http.DetectContentType
// may report for a genuine file, and the canonical MIME stored/served for it.
type typeRule struct {
	sniff     []string
	canonical string
}

const (
	mimeDocx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	mimeXlsx = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	mimePptx = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
)

// typeRules is the master list of formats the DMS knows how to verify.
// ALLOWED_EXTENSIONS can only enable a subset of these.
var typeRules = map[string]typeRule{
	"pdf":  {[]string{"application/pdf"}, "application/pdf"},
	"png":  {[]string{"image/png"}, "image/png"},
	"jpg":  {[]string{"image/jpeg"}, "image/jpeg"},
	"jpeg": {[]string{"image/jpeg"}, "image/jpeg"},
	"gif":  {[]string{"image/gif"}, "image/gif"},
	"webp": {[]string{"image/webp"}, "image/webp"},
	"txt":  {[]string{"text/plain"}, "text/plain"},
	"csv":  {[]string{"text/plain"}, "text/csv"},
	// OOXML files are ZIP containers; the sniffer can only confirm the container.
	"docx": {[]string{"application/zip"}, mimeDocx},
	"xlsx": {[]string{"application/zip"}, mimeXlsx},
	"pptx": {[]string{"application/zip"}, mimePptx},
}

// SniffLen is how many leading bytes are needed for content detection.
const SniffLen = 512

// FileRules validates uploads against the configured policy.
type FileRules struct {
	maxSize int64
	allowed map[string]struct{}
}

// NewFileRules builds the policy. It fails on extensions the DMS cannot verify.
func NewFileRules(maxSize int64, extensions []string) (*FileRules, error) {
	allowed := make(map[string]struct{}, len(extensions))
	for _, e := range extensions {
		if _, ok := typeRules[e]; !ok {
			return nil, fmt.Errorf("extension %q is not supported (supported: %s)", e, strings.Join(SupportedExtensions(), ", "))
		}
		allowed[e] = struct{}{}
	}
	return &FileRules{maxSize: maxSize, allowed: allowed}, nil
}

// SupportedExtensions lists every extension that can be enabled.
func SupportedExtensions() []string {
	out := make([]string, 0, len(typeRules))
	for e := range typeRules {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// MaxBytes is the maximum accepted file size.
func (r *FileRules) MaxBytes() int64 { return r.maxSize }

// CheckExtension returns the lower-case extension (without dot) if allowed.
func (r *FileRules) CheckExtension(name string) (string, error) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	if ext == "" {
		return "", domain.Unsupported("unsupported_file_type", "file must have an extension")
	}
	if _, ok := r.allowed[ext]; !ok {
		return "", domain.Unsupported("unsupported_file_type", fmt.Sprintf("file extension %q is not allowed", ext))
	}
	return ext, nil
}

// CheckContent verifies that the real content (not the client-declared
// Content-Type) matches the extension and returns the canonical MIME type.
func (r *FileRules) CheckContent(ext string, head []byte) (string, error) {
	if len(head) == 0 {
		return "", domain.Invalid("empty_file", "file is empty")
	}
	rule, ok := typeRules[ext]
	if !ok {
		return "", domain.Unsupported("unsupported_file_type", fmt.Sprintf("file extension %q is not allowed", ext))
	}
	detected := http.DetectContentType(head)
	if mt, _, err := mime.ParseMediaType(detected); err == nil {
		detected = mt
	}
	for _, want := range rule.sniff {
		if detected == want {
			return rule.canonical, nil
		}
	}
	return "", domain.Unsupported("content_type_mismatch",
		fmt.Sprintf("file content (%s) does not match its extension .%s", detected, ext))
}

// Previewable reports whether a MIME type is safe to render inline.
func Previewable(mimeType string) bool {
	switch {
	case mimeType == "application/pdf", mimeType == "text/plain":
		return true
	case strings.HasPrefix(mimeType, "image/"):
		return true
	}
	return false
}

// SanitizeFilename reduces a client-supplied name to a safe base name.
func SanitizeFilename(name string) (string, error) {
	name = strings.ToValidUTF8(name, "")
	name = strings.ReplaceAll(name, `\`, "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" || name == ".." {
		return "", domain.Invalid("invalid_filename", "file name is missing or invalid")
	}
	// Cap at 255 bytes without cutting a multi-byte rune in half.
	for len(name) > 255 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name, nil
}
