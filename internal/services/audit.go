package services

import (
	"context"
	"log/slog"
	"time"

	"dms/internal/domain"
	"dms/internal/repository"
)

// AuditService writes document_audit_logs. Audit failures are logged but never
// fail the user's request.
type AuditService struct {
	repo repository.AuditRepository
	log  *slog.Logger
}

func NewAuditService(repo repository.AuditRepository, log *slog.Logger) *AuditService {
	return &AuditService{repo: repo, log: log}
}

// Record logs an action performed by an authenticated caller.
func (a *AuditService) Record(ctx context.Context, p domain.Principal, action, documentID, outcome string, meta map[string]any) {
	m := make(map[string]any, len(meta)+3)
	for k, v := range meta {
		m[k] = v
	}
	if p.IP != "" {
		m["ip"] = p.IP
	}
	if p.UserAgent != "" {
		m["user_agent"] = p.UserAgent
	}
	if p.RequestID != "" {
		m["request_id"] = p.RequestID
	}

	actorID, actorType := p.ActorID, p.ActorType
	if actorID == "" {
		actorID, actorType = p.ClientID, domain.ActorTypeClient
	}
	if actorType == "" {
		actorType = domain.ActorTypeClient
	}

	a.insert(ctx, &domain.AuditLog{
		ID:          domain.NewID(),
		APIClientID: p.ClientUUID,
		DocumentID:  documentID,
		Action:      action,
		ActorID:     actorID,
		ActorType:   actorType,
		Outcome:     outcome,
		Metadata:    m,
		CreatedAt:   time.Now().UTC(),
	})
}

// RecordSystem logs an action performed by the DMS itself (e.g. the scan worker).
func (a *AuditService) RecordSystem(ctx context.Context, clientUUID, documentID, action, outcome string, meta map[string]any) {
	a.insert(ctx, &domain.AuditLog{
		ID:          domain.NewID(),
		APIClientID: clientUUID,
		DocumentID:  documentID,
		Action:      action,
		ActorID:     "dms",
		ActorType:   domain.ActorTypeSystem,
		Outcome:     outcome,
		Metadata:    meta,
		CreatedAt:   time.Now().UTC(),
	})
}

func (a *AuditService) insert(ctx context.Context, l *domain.AuditLog) {
	// Detach from the request context so a disconnecting client cannot drop the entry.
	ictx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := a.repo.Insert(ictx, l); err != nil {
		a.log.Error("write audit log failed", "action", l.Action, "document_id", l.DocumentID, "error", err)
	}
}
