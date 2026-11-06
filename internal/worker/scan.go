// Package worker runs background jobs. Currently: the malware-scan worker that
// moves quarantined (PENDING) files to CLEAN / INFECTED.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"dms/internal/config"
	"dms/internal/connector/scanner"
	"dms/internal/connector/storage"
	"dms/internal/domain"
	"dms/internal/repository"
	"dms/internal/services"
)

// ScanWorker polls the database for quarantined versions, streams each file
// from storage through the Scanner and records the verdict.
type ScanWorker struct {
	repo    repository.ScanRepository
	storage storage.Storage
	scanner scanner.Scanner
	audit   *services.AuditService
	cfg     config.ScannerConfig
	log     *slog.Logger
}

func NewScanWorker(
	repo repository.ScanRepository,
	st storage.Storage,
	sc scanner.Scanner,
	audit *services.AuditService,
	cfg config.ScannerConfig,
	log *slog.Logger,
) *ScanWorker {
	return &ScanWorker{repo: repo, storage: st, scanner: sc, audit: audit, cfg: cfg, log: log}
}

// Run blocks until ctx is cancelled.
func (w *ScanWorker) Run(ctx context.Context) {
	w.log.Info("scan worker started", "scanner", w.scanner.Name(),
		"poll_interval", w.cfg.PollInterval.String(), "batch_size", w.cfg.BatchSize)

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		w.drain(ctx)
		select {
		case <-ctx.Done():
			w.log.Info("scan worker stopped")
			return
		case <-ticker.C:
		}
	}
}

// drain keeps claiming batches until the queue is empty.
func (w *ScanWorker) drain(ctx context.Context) {
	for ctx.Err() == nil {
		jobs, err := w.repo.ClaimPending(ctx, w.cfg.BatchSize, w.cfg.StaleAfter)
		if err != nil {
			if ctx.Err() == nil {
				w.log.Error("claim scan jobs failed", "error", err)
			}
			return
		}
		if len(jobs) == 0 {
			return
		}
		for _, job := range jobs {
			if ctx.Err() != nil {
				return // unfinished SCANNING rows are reclaimed after SCAN_STALE_AFTER
			}
			w.process(ctx, job)
		}
	}
}

func (w *ScanWorker) process(ctx context.Context, job domain.ScanJob) {
	jctx, cancel := context.WithTimeout(ctx, w.cfg.Timeout)
	defer cancel()

	res, err := w.scan(jctx, job)
	if err != nil {
		w.fail(ctx, job, err)
		return
	}

	status, outcome := domain.ScanClean, domain.OutcomeSuccess
	if !res.Clean {
		status, outcome = domain.ScanInfected, domain.OutcomeFailure
	}
	if err := w.repo.CompleteScan(ctx, job, status, res.Signature); err != nil {
		// The version stays SCANNING and is reclaimed once it goes stale.
		w.log.Error("store scan result failed", "version_id", job.VersionID, "error", err)
		return
	}

	w.log.Info("scan completed", "document_id", job.DocumentID, "version_id", job.VersionID,
		"status", string(status), "signature", res.Signature)
	w.audit.RecordSystem(ctx, job.APIClientID, job.DocumentID, domain.ActionDocumentScan, outcome, map[string]any{
		"version_id": job.VersionID, "result": string(status),
		"signature": res.Signature, "scanner": w.scanner.Name(),
	})
}

func (w *ScanWorker) scan(ctx context.Context, job domain.ScanJob) (scanner.Result, error) {
	body, err := w.storage.Get(ctx, job.StorageKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return scanner.Result{}, fmt.Errorf("object %q missing from storage: %w", job.StorageKey, err)
		}
		return scanner.Result{}, fmt.Errorf("read object: %w", err)
	}
	defer body.Close()
	return w.scanner.Scan(ctx, body)
}

// fail returns the job to the queue, or marks it ERROR after too many attempts.
func (w *ScanWorker) fail(ctx context.Context, job domain.ScanJob, cause error) {
	status, err := w.repo.ReleaseFailed(ctx, job, w.cfg.MaxAttempts)
	if err != nil {
		w.log.Error("release failed scan job", "version_id", job.VersionID, "error", err)
		return
	}
	w.log.Warn("scan failed", "document_id", job.DocumentID, "version_id", job.VersionID,
		"attempt", job.Attempts, "next_status", string(status), "error", cause)

	if status == domain.ScanError {
		w.audit.RecordSystem(ctx, job.APIClientID, job.DocumentID, domain.ActionDocumentScan, domain.OutcomeFailure, map[string]any{
			"version_id": job.VersionID, "result": string(status),
			"scanner": w.scanner.Name(), "error": cause.Error(),
		})
	}
}
