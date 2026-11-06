package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"dms/internal/domain"
	"dms/internal/httpx"
	"dms/internal/middleware"
	"dms/internal/services"
)

const (
	maxFieldBytes = 64 * 1024
	// Extra room on top of the file limit for multipart framing and text fields.
	multipartOverhead = 1 << 20
)

// DocumentHandler adapts HTTP to DocumentService. It parses and shapes
// requests/responses only; business rules live in the service.
type DocumentHandler struct {
	svc          *services.DocumentService
	maxBodyBytes int64
	log          *slog.Logger
}

func NewDocumentHandler(svc *services.DocumentService, maxFileBytes int64, log *slog.Logger) *DocumentHandler {
	return &DocumentHandler{svc: svc, maxBodyBytes: maxFileBytes + multipartOverhead, log: log}
}

// Upload handles POST /api/v1/documents (multipart/form-data).
func (h *DocumentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	in, err := h.parseUpload(w, r)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	doc, err := h.svc.Upload(r.Context(), middleware.PrincipalFrom(r.Context()), *in)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusCreated, doc)
}

// UploadVersion handles POST /api/v1/documents/{documentID}/versions.
func (h *DocumentHandler) UploadVersion(w http.ResponseWriter, r *http.Request) {
	in, err := h.parseUpload(w, r)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	ver, err := h.svc.UploadVersion(r.Context(), middleware.PrincipalFrom(r.Context()), chi.URLParam(r, "documentID"), *in)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusCreated, ver)
}

func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, err := intParam(q.Get("limit"), "limit")
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	offset, err := intParam(q.Get("offset"), "offset")
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	filter := domain.DocumentFilter{
		Status:      q.Get("status"),
		ExternalRef: q.Get("external_ref"),
		Limit:       limit,
		Offset:      offset,
	}
	docs, total, err := h.svc.List(r.Context(), middleware.PrincipalFrom(r.Context()), filter)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	// The service normalises limit/offset; echo the effective values.
	eff := effectivePaging(filter)
	httpx.List(w, docs, httpx.ListMeta{Total: total, Limit: eff.Limit, Offset: eff.Offset})
}

func (h *DocumentHandler) Get(w http.ResponseWriter, r *http.Request) {
	doc, err := h.svc.Get(r.Context(), middleware.PrincipalFrom(r.Context()), chi.URLParam(r, "documentID"))
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusOK, doc)
}

// Update handles PATCH /api/v1/documents/{documentID} (external_ref, metadata).
func (h *DocumentHandler) Update(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExternalRef *string        `json:"external_ref"`
		Metadata    map[string]any `json:"metadata"`
	}
	if err := decodeJSON(w, r, &body, false); err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	doc, err := h.svc.Update(r.Context(), middleware.PrincipalFrom(r.Context()), chi.URLParam(r, "documentID"),
		services.UpdateInput{ExternalRef: body.ExternalRef, Metadata: body.Metadata})
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusOK, doc)
}

func (h *DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), middleware.PrincipalFrom(r.Context()), chi.URLParam(r, "documentID")); err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.NoContent(w)
}

func (h *DocumentHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := h.svc.ListVersions(r.Context(), middleware.PrincipalFrom(r.Context()), chi.URLParam(r, "documentID"))
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	httpx.Data(w, http.StatusOK, vs)
}

// Download handles GET /api/v1/documents/{documentID}/download[?version=n].
func (h *DocumentHandler) Download(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, domain.AccessDownload)
}

// Preview handles GET /api/v1/documents/{documentID}/preview[?version=n]
// (inline rendering for PDFs, images and plain text only).
func (h *DocumentHandler) Preview(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, domain.AccessPreview)
}

func (h *DocumentHandler) serve(w http.ResponseWriter, r *http.Request, mode domain.AccessMode) {
	version, err := intParam(r.URL.Query().Get("version"), "version")
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	dl, err := h.svc.Open(r.Context(), middleware.PrincipalFrom(r.Context()), chi.URLParam(r, "documentID"), version, mode)
	if err != nil {
		httpx.Error(w, r, h.log, err)
		return
	}
	defer dl.Body.Close()

	disposition := "attachment"
	if mode == domain.AccessPreview {
		disposition = "inline"
	}
	cd := mime.FormatMediaType(disposition, map[string]string{"filename": dl.Version.OriginalName})
	if cd == "" { // filename not representable; fall back to a bare disposition
		cd = disposition
	}

	hd := w.Header()
	hd.Set("Content-Type", dl.Version.MimeType)
	hd.Set("Content-Length", strconv.FormatInt(dl.Version.SizeBytes, 10))
	hd.Set("Content-Disposition", cd)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hd.Set("Cache-Control", "private, no-store")
	hd.Set("X-Document-Version", strconv.Itoa(dl.Version.VersionNumber))
	hd.Set("X-Checksum-SHA256", dl.Version.ChecksumSHA256)
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, dl.Body); err != nil {
		// Headers are already sent; all we can do is record it.
		h.log.Warn("stream document interrupted", "request_id", httpx.RequestID(r.Context()), "error", err)
	}
}

// parseUpload reads a multipart body as a stream. Text fields (external_ref,
// metadata, checksum_sha256) must come BEFORE the "file" part; the file part
// is returned unread so the service can stream it straight into storage.
func (h *DocumentHandler) parseUpload(w http.ResponseWriter, r *http.Request) (*services.UploadInput, error) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, domain.Invalid("invalid_multipart", "request must be multipart/form-data")
	}

	in := &services.UploadInput{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, domain.Invalid("file_required", `multipart field "file" is required`)
		}
		if err != nil {
			return nil, bodyError(err)
		}

		switch part.FormName() {
		case "file":
			in.Filename = part.FileName()
			if in.Filename == "" {
				return nil, domain.Invalid("file_required", `multipart field "file" must be a file upload`)
			}
			in.Body = part
			return in, nil

		case "external_ref":
			v, err := readField(part)
			if err != nil {
				return nil, err
			}
			in.ExternalRef = strings.TrimSpace(v)

		case "checksum_sha256":
			v, err := readField(part)
			if err != nil {
				return nil, err
			}
			in.ChecksumSHA256 = strings.TrimSpace(v)

		case "metadata":
			v, err := readField(part)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(v) != "" {
				var m map[string]any
				if err := json.Unmarshal([]byte(v), &m); err != nil {
					return nil, domain.Invalid("invalid_metadata", "metadata must be a JSON object")
				}
				in.Metadata = m
			}

		default:
			// Unknown fields are ignored but must be drained to advance the reader.
			_, _ = io.Copy(io.Discard, io.LimitReader(part, maxFieldBytes))
		}
	}
}

func readField(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxFieldBytes+1))
	if err != nil {
		return "", bodyError(err)
	}
	if len(b) > maxFieldBytes {
		return "", domain.Invalid("field_too_large", "form field exceeds 64KB")
	}
	return string(b), nil
}

// bodyError maps request-body read errors to application errors.
func bodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return domain.TooLarge("request body exceeds the maximum allowed size")
	}
	return domain.Invalid("invalid_multipart", "malformed multipart body")
}

func intParam(v, name string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, domain.Invalid("invalid_"+name, name+" must be a non-negative integer")
	}
	return n, nil
}

// effectivePaging mirrors the service's defaults so responses report real values.
func effectivePaging(f domain.DocumentFilter) domain.DocumentFilter {
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 100 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	return f
}
