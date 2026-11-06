package domain

import "time"

// ---- API clients -----------------------------------------------------------

type ClientStatus string

const (
	ClientActive    ClientStatus = "ACTIVE"
	ClientSuspended ClientStatus = "SUSPENDED"
)

type KeyStatus string

const (
	KeyActive  KeyStatus = "ACTIVE"
	KeyRevoked KeyStatus = "REVOKED"
)

// APIClient is a consuming application (one tenant of the DMS).
type APIClient struct {
	ID        string       `json:"id"`
	ClientID  string       `json:"client_id"`
	Name      string       `json:"name"`
	Status    ClientStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// APIClientKey is a credential of an APIClient. Only the SHA-256 hash of the
// key is stored; the plaintext is shown exactly once when it is issued.
type APIClientKey struct {
	ID          string     `json:"id"`
	APIClientID string     `json:"api_client_id"`
	KeyPrefix   string     `json:"key_prefix"`
	KeyHash     string     `json:"-"`
	Status      KeyStatus  `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ---- Documents -------------------------------------------------------------

// ScanStatus is the malware-scan state of a single file version.
type ScanStatus string

const (
	ScanPending  ScanStatus = "PENDING"
	ScanScanning ScanStatus = "SCANNING"
	ScanClean    ScanStatus = "CLEAN"
	ScanInfected ScanStatus = "INFECTED"
	ScanError    ScanStatus = "ERROR"
)

// DocumentStatus is the lifecycle state of a document as a whole.
//
//	PENDING   no version has passed the scan yet
//	AVAILABLE at least one version is CLEAN and can be served
//	REJECTED  every version was INFECTED or failed scanning
//	DELETED   soft-deleted by the owning client
type DocumentStatus string

const (
	DocPending   DocumentStatus = "PENDING"
	DocAvailable DocumentStatus = "AVAILABLE"
	DocRejected  DocumentStatus = "REJECTED"
	DocDeleted   DocumentStatus = "DELETED"
)

// Document mirrors the latest uploaded version. Older versions live in
// DocumentVersion.
type Document struct {
	ID             string         `json:"id"`
	APIClientID    string         `json:"-"`
	ExternalRef    string         `json:"external_ref,omitempty"`
	OriginalName   string         `json:"original_name"`
	MimeType       string         `json:"mime_type"`
	SizeBytes      int64          `json:"size_bytes"`
	ChecksumSHA256 string         `json:"checksum_sha256"`
	StorageKey     string         `json:"-"`
	ScanStatus     ScanStatus     `json:"scan_status"`
	Status         DocumentStatus `json:"status"`
	CurrentVersion int            `json:"current_version"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type DocumentVersion struct {
	ID             string     `json:"id"`
	DocumentID     string     `json:"document_id"`
	VersionNumber  int        `json:"version_number"`
	OriginalName   string     `json:"original_name"`
	MimeType       string     `json:"mime_type"`
	SizeBytes      int64      `json:"size_bytes"`
	ChecksumSHA256 string     `json:"checksum_sha256"`
	StorageKey     string     `json:"-"`
	ScanStatus     ScanStatus `json:"scan_status"`
	ScanSignature  string     `json:"scan_signature,omitempty"`
	ScanAttempts   int        `json:"-"`
	ScannedAt      *time.Time `json:"scanned_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// DocumentFilter drives Document listing.
type DocumentFilter struct {
	Status      string
	ExternalRef string
	Limit       int
	Offset      int
}

// ScanJob is a version claimed by the scan worker.
type ScanJob struct {
	VersionID   string
	DocumentID  string
	APIClientID string
	StorageKey  string
	Attempts    int
}

// AccessMode distinguishes an attachment download from an inline preview.
type AccessMode string

const (
	AccessDownload AccessMode = "download"
	AccessPreview  AccessMode = "preview"
)

// ---- Audit -----------------------------------------------------------------

const (
	ActionDocumentUpload        = "DOCUMENT_UPLOAD"
	ActionDocumentVersionUpload = "DOCUMENT_VERSION_UPLOAD"
	ActionDocumentUpdate        = "DOCUMENT_UPDATE"
	ActionDocumentDelete        = "DOCUMENT_DELETE"
	ActionDocumentDownload      = "DOCUMENT_DOWNLOAD"
	ActionDocumentPreview       = "DOCUMENT_PREVIEW"
	ActionDocumentScan          = "DOCUMENT_SCAN"
)

const (
	OutcomeSuccess = "SUCCESS"
	OutcomeDenied  = "DENIED"
	OutcomeFailure = "FAILURE"
)

const (
	ActorTypeClient = "CLIENT"
	ActorTypeUser   = "USER"
	ActorTypeSystem = "SYSTEM"
)

type AuditLog struct {
	ID          string
	APIClientID string
	DocumentID  string // empty = not tied to a document
	Action      string
	ActorID     string
	ActorType   string
	Outcome     string
	Metadata    map[string]any
	CreatedAt   time.Time
}

// Principal is the authenticated caller plus request context, passed
// explicitly from the HTTP layer into services.
type Principal struct {
	ClientUUID string // api_clients.id
	ClientID   string // api_clients.client_id (public identifier)
	KeyID      string
	ActorID    string // optional end-user id supplied by the calling app
	ActorType  string
	IP         string
	UserAgent  string
	RequestID  string
}
