package document

import "time"

type Document struct {
	ID             string    `json:"id"`
	OriginalName   string    `json:"originalName"`
	MimeType       string    `json:"mimeType"`
	SizeBytes      int64     `json:"sizeBytes"`
	ChecksumSHA256 string    `json:"checksumSha256,omitempty"`
	StorageKey     string    `json:"-"`
	CurrentVersion int       `json:"currentVersion"`
	DocumentType   string    `json:"documentType,omitempty"`
	EntityType     string    `json:"entityType,omitempty"`
	EntityID       string    `json:"entityId,omitempty"`
	MerchantID     string    `json:"merchantId"`
	RegistrationID string    `json:"registrationId,omitempty"`
	Status         string    `json:"status"`
	UploadedBy     string    `json:"uploadedBy,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
