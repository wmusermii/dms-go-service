package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrFileRequired   = errors.New("file is required")
	ErrFileTooLarge   = errors.New("file size exceeds maximum allowed size")
	ErrInvalidMIME    = errors.New("mime type is not allowed")
	ErrInvalidRequest = errors.New("invalid request")
)

type Service struct {
	repository  *Repository
	storageRoot string
	maxFileSize int64
}

func NewService(repository *Repository, storageRoot string, maxFileSize int64) *Service {
	return &Service{
		repository:  repository,
		storageRoot: storageRoot,
		maxFileSize: maxFileSize,
	}
}

func (s *Service) Upload(
	ctx context.Context,
	fileHeader *multipart.FileHeader,
	documentType string,
	entityType string,
	entityID string,
	merchantID string,
	registrationID string,
	uploadedBy string,
) (*Document, error) {
	if fileHeader == nil || fileHeader.Size == 0 {
		return nil, ErrFileRequired
	}

	if fileHeader.Size > s.maxFileSize {
		return nil, ErrFileTooLarge
	}

	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	if !isAllowedMIME(mimeType) {
		return nil, ErrInvalidMIME
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("open uploaded file: %w", err)
	}
	defer file.Close()

	checksum, err := calculateSHA256(file)
	if err != nil {
		return nil, fmt.Errorf("calculate checksum: %w", err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reset file reader: %w", err)
	}

	documentID := uuid.NewString()

	storageKey := buildStorageKey(
		documentID,
		fileHeader.Filename,
	)

	absolutePath := filepath.Join(s.storageRoot, storageKey)

	if err := os.MkdirAll(filepath.Dir(absolutePath), 0755); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	if err := saveFile(absolutePath, file); err != nil {
		return nil, fmt.Errorf("save uploaded file: %w", err)
	}

	doc := &Document{
		OriginalName:   filepath.Base(fileHeader.Filename),
		MimeType:       mimeType,
		SizeBytes:      fileHeader.Size,
		ChecksumSHA256: checksum,
		StorageKey:     storageKey,
		CurrentVersion: 1,
		DocumentType:   documentType,
		EntityType:     entityType,
		EntityID:       entityID,
		MerchantID:     merchantID,
		RegistrationID: registrationID,
		Status:         "ACTIVE",
		UploadedBy:     uploadedBy,
	}

	if err := s.repository.Create(ctx, doc); err != nil {
		_ = os.Remove(absolutePath)
		return nil, fmt.Errorf("create document metadata: %w", err)
	}

	return doc, nil
}

func (s *Service) GetByID(ctx context.Context, id string) (*Document, error) {
	return s.repository.GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]Document, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if offset < 0 {
		offset = 0
	}

	return s.repository.List(ctx, limit, offset)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repository.SoftDelete(ctx, id)
}

func (s *Service) GetFilePath(ctx context.Context, id string) (string, error) {
	storageKey, err := s.repository.GetStorageKey(ctx, id)
	if err != nil {
		return "", err
	}

	filePath := filepath.Join(s.storageRoot, storageKey)

	if _, err := os.Stat(filePath); err != nil {
		return "", fmt.Errorf("physical file not found: %w", err)
	}

	return filePath, nil
}

func calculateSHA256(file multipart.File) (string, error) {
	hasher := sha256.New()

	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func buildStorageKey(documentID, originalName string) string {
	now := time.Now().UTC()

	extension := filepath.Ext(originalName)
	fileName := documentID + "_v1" + strings.ToLower(extension)

	return filepath.Join(
		fmt.Sprintf("%d", now.Year()),
		fmt.Sprintf("%02d", int(now.Month())),
		fmt.Sprintf("%02d", now.Day()),
		documentID,
		fileName,
	)
}

func saveFile(destination string, source multipart.File) error {
	target, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer target.Close()

	_, err = io.Copy(target, source)

	return err
}

func isAllowedMIME(mimeType string) bool {
	allowed := map[string]bool{
		"application/pdf": true,
		"image/jpeg":      true,
		"image/png":       true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       true,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	}

	return allowed[strings.ToLower(mimeType)]
}
