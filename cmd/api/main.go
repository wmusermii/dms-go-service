// Command api runs the DMS HTTP API and the background scan worker.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dms/internal/config"
	"dms/internal/connector/scanner"
	"dms/internal/connector/storage"
	"dms/internal/database"
	"dms/internal/handlers"
	"dms/internal/repository/postgres"
	"dms/internal/routes"
	"dms/internal/services"
	"dms/internal/validation"
	"dms/internal/worker"
	"dms/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ---- infrastructure ----
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.MigrateOnStart {
		if err := database.Migrate(ctx, pool, migrations.FS, log); err != nil {
			return err
		}
	}

	store, err := storage.New(ctx, cfg.Storage)
	if err != nil {
		return err
	}
	scn, err := scanner.New(cfg.Scanner.Driver)
	if err != nil {
		return err
	}
	rules, err := validation.NewFileRules(cfg.MaxUploadBytes, cfg.AllowedExtensions)
	if err != nil {
		return fmt.Errorf("ALLOWED_EXTENSIONS: %w", err)
	}

	// ---- repositories ----
	clientRepo := postgres.NewClientRepo(pool)
	keyRepo := postgres.NewKeyRepo(pool)
	docRepo := postgres.NewDocumentRepo(pool)
	scanRepo := postgres.NewScanRepo(pool)
	auditRepo := postgres.NewAuditRepo(pool)

	// ---- services ----
	auditSvc := services.NewAuditService(auditRepo, log)
	authSvc := services.NewAuthService(keyRepo)
	clientSvc := services.NewClientService(clientRepo, keyRepo)
	docSvc := services.NewDocumentService(docRepo, store, rules, auditSvc, log)

	// ---- background worker ----
	workerDone := make(chan struct{})
	if scn != nil {
		if cfg.Scanner.Driver == "noop" {
			log.Warn("SCANNER_DRIVER=noop marks every file CLEAN without scanning; never use in production")
		}
		w := worker.NewScanWorker(scanRepo, store, scn, auditSvc, cfg.Scanner, log)
		go func() {
			defer close(workerDone)
			w.Run(ctx)
		}()
	} else {
		close(workerDone)
		log.Warn("no scanner configured (SCANNER_DRIVER=disabled): uploaded files stay PENDING and cannot be downloaded")
	}

	// ---- http ----
	deps := routes.Deps{
		Log:        log,
		Auth:       authSvc,
		Health:     handlers.NewHealthHandler(pool),
		Documents:  handlers.NewDocumentHandler(docSvc, cfg.MaxUploadBytes, log),
		TrustProxy: cfg.TrustProxy,
	}
	if cfg.AdminToken != "" {
		deps.Admin = handlers.NewAdminHandler(clientSvc, log)
		deps.AdminToken = cfg.AdminToken
	} else {
		log.Warn("ADMIN_TOKEN is empty: /admin/v1 endpoints are disabled")
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           routes.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No Read/WriteTimeout: they would cut off large uploads and downloads.
	}

	serveErr := make(chan error, 1)
	go func() {
		log.Info("dms api listening", "addr", cfg.HTTPAddr, "env", cfg.AppEnv,
			"storage", store.Name(), "max_upload_bytes", cfg.MaxUploadBytes)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		stop()
		<-workerDone
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http shutdown: %w", err)
	}
	<-workerDone
	return nil
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(strings.ToLower(level))); err != nil {
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}
