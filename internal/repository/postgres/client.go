package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"dms/internal/domain"
)

const clientCols = `id::text, client_id, name, status, created_at, updated_at`

func scanClient(s scanner, c *domain.APIClient) error {
	var status string
	if err := s.Scan(&c.ID, &c.ClientID, &c.Name, &status, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return err
	}
	c.Status = domain.ClientStatus(status)
	return nil
}

type ClientRepo struct{ pool *pgxpool.Pool }

func NewClientRepo(pool *pgxpool.Pool) *ClientRepo { return &ClientRepo{pool: pool} }

func (r *ClientRepo) Create(ctx context.Context, c *domain.APIClient) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_clients (id, client_id, name, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $5)`,
		c.ID, c.ClientID, c.Name, string(c.Status), c.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Conflict("client_exists", "client_id is already in use")
		}
		return fmt.Errorf("insert api_client: %w", err)
	}
	return nil
}

func (r *ClientRepo) List(ctx context.Context) ([]domain.APIClient, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+clientCols+` FROM api_clients ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list api_clients: %w", err)
	}
	defer rows.Close()

	out := []domain.APIClient{}
	for rows.Next() {
		var c domain.APIClient
		if err := scanClient(rows, &c); err != nil {
			return nil, fmt.Errorf("scan api_client: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ClientRepo) GetByID(ctx context.Context, id string) (*domain.APIClient, error) {
	var c domain.APIClient
	err := scanClient(r.pool.QueryRow(ctx, `SELECT `+clientCols+` FROM api_clients WHERE id = $1`, id), &c)
	if err != nil {
		if isNoRows(err) {
			return nil, domain.NotFound("client not found")
		}
		return nil, fmt.Errorf("get api_client: %w", err)
	}
	return &c, nil
}
