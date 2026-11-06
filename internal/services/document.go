package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"dms/internal/connector/storage"
	"dms/internal/domain"
	"dms/internal/repository"
	"dms/internal/validation"
)

const (
	maxExternalRefLen = 255
	maxMetadataBytes  = 16 * 1024
	defaultPageSize   = 20
	maxPageSize       = 100
)

// DocumentService implements the document use-cases: upload (validate →
// quarantine), versioning, metadata, listing and gated download/preview.
type DocumentService struct {
	docs    repository.DocumentRepository
	storage storage.Storage
	rules   *validation.FileRules
	audit   *AuditService
	log     *slog.Logger
}

func NewDocumentService(
	docs repository.DocumentRepository,
	st storage.Storage,
	rules *validation.FileRules,
	audit *AuditService,
	log *slog.Logger,
) *DocumentService {
	return &DocumentService{docs: docs, storage: st, rules: rules, audit: audit, log: log}
}

// UploadInput carries one file stream plus its descriptors.
type UploadInput struct {
	Filename       string
	Body           io.Reader
	ExternalRef    string
	Metadata       map[string]any
	ChecksumSHA256 string // optional; verified against the stored bytes
}

// UpdateInput changes descriptors only; nil fields are left untouched.
type UpdateInput struct {
	ExternalRef *string
	Metadata    map[string]any
}

// Download is an opened, scan-cleared file. The caller must close Body.
type Download struct {
	Version *domain.DocumentVersion
	Body    io.ReadCloser
}

// storedFile is the result of validating and writing one upload.
type storedFile struct {
	Name     string
	MimeType string
	Size     int64
	Checksum string
	Key      string
}

// ---- Upload ----------------------------------------------------------------

// Upload validates the stream, writes it to private storage in quarantine and
// records the document as PENDING. The scan worker later promotes it.
func (s *DocumentService) Upload(ctx context.Context, p domain.Principal, in UploadInput) (*domain.Document, error) {
	if err := validateDescriptors(in.ExternalRef, in.Metadata); err != nil {
		return nil, err
	}

	docID, versionID := domain.NewID(), domain.NewID()
	sf, err := s.store(ctx, p.ClientUUID, docID, versionID, in)
	if err != nil {
		s.audit.Record(ctx, p, domain.ActionDocumentUpload, "", domain.OutcomeFailure, map[string]any{"reason": reasonOf(err)})
		return nil, err
	}

	now := time.Now().UTC()
	doc := &domain.Document{
		ID: docID, APIClientID: p.ClientUUID, ExternalRef: in.ExternalRef,
		OriginalName: sf.Name, MimeType: sf.MimeType, SizeBytes: sf.Size,
		ChecksumSHA256: sf.Checksum, StorageKey: sf.Key,
		ScanStatus: domain.ScanPending, Status: domain.DocPending, CurrentVersion: 1,
		Metadata: orEmpty(in.Metadata), CreatedAt: now, UpdatedAt: now,
	}
	ver := &domain.DocumentVersion{
		ID: versionID, DocumentID: docID, VersionNumber: 1,
		OriginalName: sf.Name, MimeType: sf.MimeType, SizeBytes: sf.Size,
		ChecksumSHA256: sf.Checksum, StorageKey: sf.Key,
		ScanStatus: domain.ScanPending, CreatedAt: now,
	}
	if err := s.docs.CreateWithVersion(ctx, doc, ver); err != nil {
		s.cleanup(sf.Key)
		s.audit.Record(ctx, p, domain.ActionDocumentUpload, "", domain.OutcomeFailure, map[string]any{"reason": "database_error"})
		return nil, asAppError(err)
	}

	s.audit.Record(ctx, p, domain.ActionDocumentUpload, docID, domain.OutcomeSuccess, map[string]any{
		"version": 1, "size_bytes": sf.Size, "mime_type": sf.MimeType, "checksum_sha256": sf.Checksum,
	})
	return doc, nil
}

// UploadVersion adds a new version to an existing document.
func (s *DocumentService) UploadVersion(ctx context.Context, p domain.Principal, docID string, in UploadInput) (*domain.DocumentVersion, error) {
	if !domain.IsUUID(docID) {
		return nil, domain.NotFound("document not found")
	}
	// Fail fast, before streaming a large body into storage.
	if _, err := s.docs.GetByID(ctx, p.ClientUUID, docID); err != nil {
		return nil, asAppError(err)
	}

	versionID := domain.NewID()
	sf, err := s.store(ctx, p.ClientUUID, docID, versionID, in)
	if err != nil {
		s.audit.Record(ctx, p, domain.ActionDocumentVersionUpload, docID, domain.OutcomeFailure, map[string]any{"reason": reasonOf(err)})
		return nil, err
	}

	ver := &domain.DocumentVersion{
		ID: versionID, DocumentID: docID,
		OriginalName: sf.Name, MimeType: sf.MimeType, SizeBytes: sf.Size,
		ChecksumSHA256: sf.Checksum, StorageKey: sf.Key,
		ScanStatus: domain.ScanPending, CreatedAt: time.Now().UTC(),
	}
	if err := s.docs.AddVersion(ctx, p.ClientUUID, docID, ver); err != nil {
		s.cleanup(sf.Key)
		s.audit.Record(ctx, p, domain.ActionDocumentVersionUpload, docID, domain.OutcomeFailure, map[string]any{"reason": "database_error"})
		return nil, asAppError(err)
	}

	s.audit.Record(ctx, p, domain.ActionDocumentVersionUpload, docID, domain.OutcomeSuccess, map[string]any{
		"version": ver.VersionNumber, "size_bytes": sf.Size, "mime_type": sf.MimeType, "checksum_sha256": sf.Checksum,
	})
	return ver, nil
}

// store runs the validation pipeline (size → extension → content sniffing →
// checksum) while streaming the body into storage, never buffering the file.
func (s *DocumentService) store(ctx context.Context, clientUUID, docID, versionID string, in UploadInput) (*storedFile, error) {
	name, err := validation.SanitizeFilename(in.Filename)
	if err != nil {
		return nil, err
	}
	ext, err := s.rules.CheckExtension(name)
	if err != nil {
		return nil, err
	}
	if in.ChecksumSHA256 != "" && !isHexSHA256(in.ChecksumSHA256) {
		return nil, domain.Invalid("invalid_checksum", "checksum_sha256 must be 64 hex characters")
	}

	// Sniff the real content type from the first bytes, ignoring the client's Content-Type.
	limited := validation.NewLimitedReader(in.Body, s.rules.MaxBytes())
	br := bufio.NewReaderSize(limited, validation.SniffLen)
	head, perr := br.Peek(validation.SniffLen)
	if perr != nil && !errors.Is(perr, io.EOF) {
		return nil, s.mapReadError(limited, perr)
	}
	mimeType, err := s.rules.CheckContent(ext, head)
	if err != nil {
		return nil, err
	}

	hr := validation.NewHashReader(br)
	key := fmt.Sprintf("clients/%s/documents/%s/%s", clientUUID, docID, versionID)
	if err := s.storage.Put(ctx, key, hr, -1, mimeType); err != nil {
		return nil, s.mapReadError(limited, err)
	}

	sum := hr.Sum()
	if in.ChecksumSHA256 != "" && !strings.EqualFold(in.ChecksumSHA256, sum) {
		s.cleanup(key)
		return nil, domain.Invalid("checksum_mismatch", "checksum_sha256 does not match the uploaded content")
	}
	return &storedFile{Name: name, MimeType: mimeType, Size: hr.Size(), Checksum: sum, Key: key}, nil
}

func (s *DocumentService) mapReadError(lr *validation.LimitedReader, err error) error {
	if lr.Exceeded() || errors.Is(err, validation.ErrTooLarge) {
		return domain.TooLarge(fmt.Sprintf("file exceeds the maximum size of %d bytes", s.rules.MaxBytes()))
	}
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return domain.Internal(err)
}

// cleanup removes an orphaned object, detached from the request context.
func (s *DocumentService) cleanup(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.storage.Delete(ctx, key); err != nil {
		s.log.Error("cleanup orphaned object failed", "key", key, "error", err)
	}
}

// ---- Read / update / delete ------------------------------------------------

func (s *DocumentService) Get(ctx context.Context, p domain.Principal, id string) (*domain.Document, error) {
	if !domain.IsUUID(id) {
		return nil, domain.NotFound("document not found")
	}
	d, err := s.docs.GetByID(ctx, p.ClientUUID, id)
	return d, asAppError(err)
}

func (s *DocumentService) List(ctx context.Context, p domain.Principal, f domain.DocumentFilter) ([]domain.Document, int, error) {
	f.Status = strings.ToUpper(strings.TrimSpace(f.Status))
	switch f.Status {
	case "", string(domain.DocPending), string(domain.DocAvailable), string(domain.DocRejected):
	default:
		return nil, 0, domain.Invalid("invalid_status", "status must be PENDING, AVAILABLE or REJECTED")
	}
	if f.Limit <= 0 {
		f.Limit = defaultPageSize
	}
	if f.Limit > maxPageSize {
		f.Limit = maxPageSize
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	docs, total, err := s.docs.List(ctx, p.ClientUUID, f)
	return docs, total, asAppError(err)
}

func (s *DocumentService) ListVersions(ctx context.Context, p domain.Principal, id string) ([]domain.DocumentVersion, error) {
	if !domain.IsUUID(id) {
		return nil, domain.NotFound("document not found")
	}
	if _, err := s.docs.GetByID(ctx, p.ClientUUID, id); err != nil {
		return nil, asAppError(err)
	}
	vs, err := s.docs.ListVersions(ctx, p.ClientUUID, id)
	return vs, asAppError(err)
}

func (s *DocumentService) Update(ctx context.Context, p domain.Principal, id string, in UpdateInput) (*domain.Document, error) {
	if !domain.IsUUID(id) {
		return nil, domain.NotFound("document not found")
	}
	doc, err := s.docs.GetByID(ctx, p.ClientUUID, id)
	if err != nil {
		return nil, asAppError(err)
	}

	if in.ExternalRef != nil {
		doc.ExternalRef = strings.TrimSpace(*in.ExternalRef)
	}
	if in.Metadata != nil {
		doc.Metadata = in.Metadata
	}
	if err := validateDescriptors(doc.ExternalRef, doc.Metadata); err != nil {
		return nil, err
	}
	if err := s.docs.UpdateDescriptors(ctx, p.ClientUUID, doc); err != nil {
		return nil, asAppError(err)
	}

	s.audit.Record(ctx, p, domain.ActionDocumentUpdate, id, domain.OutcomeSuccess, nil)
	updated, err := s.docs.GetByID(ctx, p.ClientUUID, id)
	return updated, asAppError(err)
}

// Delete soft-deletes the document. Stored objects are retained; physical
// purging (retention policy) is intentionally a separate concern.
func (s *DocumentService) Delete(ctx context.Context, p domain.Principal, id string) error {
	if !domain.IsUUID(id) {
		return domain.NotFound("document not found")
	}
	if err := s.docs.SoftDelete(ctx, p.ClientUUID, id); err != nil {
		return asAppError(err)
	}
	s.audit.Record(ctx, p, domain.ActionDocumentDelete, id, domain.OutcomeSuccess, nil)
	return nil
}

// ---- Download / preview ----------------------------------------------------

// Open returns a document file for download or inline preview. Only versions
// whose scan verdict is CLEAN are ever served. version 0 selects the latest
// clean version.
func (s *DocumentService) Open(ctx context.Context, p domain.Principal, id string, version int, mode domain.AccessMode) (*Download, error) {
	action := domain.ActionDocumentDownload
	if mode == domain.AccessPreview {
		action = domain.ActionDocumentPreview
	}
	if !domain.IsUUID(id) {
		return nil, domain.NotFound("document not found")
	}

	doc, err := s.docs.GetByID(ctx, p.ClientUUID, id)
	if err != nil {
		return nil, asAppError(err)
	}

	var ver *domain.DocumentVersion
	if version > 0 {
		ver, err = s.docs.GetVersion(ctx, p.ClientUUID, id, version)
		if err != nil {
			return nil, asAppError(err)
		}
		if ver.ScanStatus != domain.ScanClean {
			s.audit.Record(ctx, p, action, id, domain.OutcomeDenied, map[string]any{"version": ver.VersionNumber, "scan_status": string(ver.ScanStatus)})
			return nil, domain.NotAvailable(fmt.Sprintf("version %d is not available (scan status: %s)", ver.VersionNumber, ver.ScanStatus))
		}
	} else {
		ver, err = s.docs.GetLatestCleanVersion(ctx, p.ClientUUID, id)
		if err != nil {
			var appErr *domain.AppError
			if errors.As(err, &appErr) && appErr.Kind == domain.KindNotFound {
				s.audit.Record(ctx, p, action, id, domain.OutcomeDenied, map[string]any{"scan_status": string(doc.ScanStatus)})
				return nil, domain.NotAvailable(fmt.Sprintf("document is not available (status: %s, scan status: %s)", doc.Status, doc.ScanStatus))
			}
			return nil, asAppError(err)
		}
	}

	if mode == domain.AccessPreview && !validation.Previewable(ver.MimeType) {
		return nil, domain.Unsupported("preview_not_supported", "preview is not supported for this file type; use download")
	}

	body, err := s.storage.Get(ctx, ver.StorageKey)
	if err != nil {
		s.audit.Record(ctx, p, action, id, domain.OutcomeFailure, map[string]any{"version": ver.VersionNumber, "reason": "storage_error"})
		if errors.Is(err, storage.ErrNotFound) {
			return nil, domain.Internal(fmt.Errorf("object missing for version %s: %w", ver.ID, err))
		}
		return nil, domain.Internal(err)
	}

	s.audit.Record(ctx, p, action, id, domain.OutcomeSuccess, map[string]any{"version": ver.VersionNumber})
	return &Download{Version: ver, Body: body}, nil
}

// ---- helpers ---------------------------------------------------------------

func validateDescriptors(externalRef string, metadata map[string]any) error {
	if len(externalRef) > maxExternalRefLen {
		return domain.Invalid("invalid_external_ref", fmt.Sprintf("external_ref must be at most %d characters", maxExternalRefLen))
	}
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			return domain.Invalid("invalid_metadata", "metadata must be a JSON object")
		}
		if len(raw) > maxMetadataBytes {
			return domain.Invalid("invalid_metadata", fmt.Sprintf("metadata must be at most %d bytes", maxMetadataBytes))
		}
	}
	return nil
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// asAppError passes AppErrors through and wraps anything else as Internal.
// A nil error stays nil.
func asAppError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return domain.Internal(err)
}

func reasonOf(err error) string {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return "internal_error"
}
