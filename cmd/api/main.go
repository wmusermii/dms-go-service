package main

import (
	"context"
	"log"
	"net/http"

	"github.com/wmusermii/dms-go-service/internal/auth"
	"github.com/wmusermii/dms-go-service/internal/config"
	"github.com/wmusermii/dms-go-service/internal/database"
	"github.com/wmusermii/dms-go-service/internal/document"
	"github.com/wmusermii/dms-go-service/internal/httpapi"
)

func main() {
	cfg := config.MustLoad()

	ctx := context.Background()

	pool, err := database.NewPostgresPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	documentRepository := document.NewRepository(pool)

	const maxFileSize = 25 << 20

	documentService := document.NewService(
		documentRepository,
		cfg.StorageRoot,
		maxFileSize,
	)

	documentHandler := httpapi.NewDocumentHandler(documentService)

	tokenService, err := auth.NewTokenService(
		cfg.JWTPrivateKeyPath,
		cfg.JWTIssuer,
		cfg.JWTAudience,
		cfg.JWTExpiry,
	)
	if err != nil {
		log.Fatal(err)
	}

	authHandler := httpapi.NewAuthHandler(tokenService)

	apiKeyMiddleware := httpapi.NewAPIKeyMiddleware(cfg.APIKey)

	authMiddleware, err := httpapi.NewAuthMiddleware(httpapi.AuthConfig{
		APIKey:        cfg.APIKey,
		PublicKeyPath: cfg.JWTPublicKeyPath,
		Issuer:        cfg.JWTIssuer,
		Audience:      cfg.JWTAudience,
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("GET /health/db", func(w http.ResponseWriter, r *http.Request) {
		var result int

		if err := pool.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"error","database":"unavailable"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","database":"connected"}`))
	})

	mux.Handle("POST /v1/auth/token", apiKeyMiddleware.Middleware(http.HandlerFunc(authHandler.IssueToken)))
	protected := http.NewServeMux()
	protected.HandleFunc("POST /v1/documents/bulk", documentHandler.UploadBulk)
	protected.HandleFunc("POST /v1/documents", documentHandler.Upload)
	protected.HandleFunc("GET /v1/documents", documentHandler.List)
	protected.HandleFunc("GET /v1/documents/{id}", documentHandler.GetByID)
	protected.HandleFunc("GET /v1/documents/{id}/download", documentHandler.Download)
	protected.HandleFunc("GET /v1/documents/{id}/preview", documentHandler.Preview)
	protected.HandleFunc("DELETE /v1/documents/{id}", documentHandler.Delete)

	mux.Handle("/v1/", authMiddleware.Middleware(protected))

	addr := ":" + cfg.AppPort

	log.Printf("DMS service running on %s", addr)
	log.Printf("Storage root: %s", cfg.StorageRoot)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
