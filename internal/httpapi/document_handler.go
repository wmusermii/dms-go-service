package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/wmusermii/dms-go-service/internal/document"
)

type DocumentHandler struct {
	service *document.Service
}

func NewDocumentHandler(service *document.Service) *DocumentHandler {
	return &DocumentHandler{service: service}
}

func (h *DocumentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 10 << 20 // 10 MB

	if err := r.ParseMultipartForm(maxMemory); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	documentType := r.FormValue("documentType")
	entityType := r.FormValue("entityType")
	entityID := r.FormValue("entityId")
	uploadedBy := r.FormValue("uploadedBy")

	doc, err := h.service.Upload(
		r.Context(),
		fileHeader,
		documentType,
		entityType,
		entityID,
		uploadedBy,
	)

	if errors.Is(err, document.ErrFileRequired) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if errors.Is(err, document.ErrFileTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}

	if errors.Is(err, document.ErrInvalidMIME) {
		writeError(w, http.StatusUnsupportedMediaType, err.Error())
		return
	}

	if err != nil {
		log.Printf(
			"upload document failed: %v | file=%s size=%d mime=%s",
			err,
			fileHeader.Filename,
			fileHeader.Size,
			fileHeader.Header.Get("Content-Type"),
		)

		writeError(w, http.StatusInternalServerError, "failed to upload document")
		return
	}

	writeJSON(w, http.StatusCreated, doc)
}

func (h *DocumentHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	doc, err := h.service.GetByID(r.Context(), id)

	if errors.Is(err, document.ErrNotFound) {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	if err != nil {
		log.Printf("get document failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to get document")
		return
	}

	writeJSON(w, http.StatusOK, doc)
}

func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 20)
	offset := parseIntQuery(r, "offset", 0)

	documents, err := h.service.List(r.Context(), limit, offset)
	if err != nil {
		log.Printf("list documents failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}

	writeJSON(w, http.StatusOK, documents)
}

func (h *DocumentHandler) Download(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	filePath, err := h.service.GetFilePath(r.Context(), id)

	if errors.Is(err, document.ErrNotFound) {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	if err != nil {
		log.Printf("download document failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to download document")
		return
	}

	doc, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to download document")
		return
	}

	w.Header().Set("Content-Type", doc.MimeType)
	w.Header().Set(
		"Content-Disposition",
		`attachment; filename="`+doc.OriginalName+`"`,
	)

	http.ServeFile(w, r, filePath)
}

func (h *DocumentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	err := h.service.Delete(r.Context(), id)

	if errors.Is(err, document.ErrNotFound) {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	if err != nil {
		log.Printf("delete document failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to delete document")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write json response failed: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}
func (h *DocumentHandler) Preview(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	doc, err := h.service.GetByID(r.Context(), id)

	if errors.Is(err, document.ErrNotFound) {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	if err != nil {
		log.Printf("preview document failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to preview document")
		return
	}

	filePath, err := h.service.GetFilePath(r.Context(), id)

	if errors.Is(err, document.ErrNotFound) {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	if err != nil {
		log.Printf("preview document failed: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to preview document")
		return
	}

	w.Header().Set("Content-Type", doc.MimeType)
	w.Header().Set(
		"Content-Disposition",
		`inline; filename="`+doc.OriginalName+`"`,
	)
	w.Header().Set("Cache-Control", "private, max-age=300")

	http.ServeFile(w, r, filePath)
}
