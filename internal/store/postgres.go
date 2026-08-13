package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/obiente/support/internal/domain"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func OpenPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (postgres *Postgres) Create(ctx context.Context, report domain.Report) error {
	_, err := postgres.pool.Exec(ctx, `
		INSERT INTO support_reports (
			id, support_code, product_id, request_type, status, private_payload,
			capability_ciphertext, diagnostic_object_key, idempotency_hash, request_hash,
			capability_hash, created_at, updated_at, retention_until
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		report.ID, report.SupportCode, report.ProductID, report.RequestType, report.Status,
		report.PrivatePayload, report.CapabilityCiphertext, report.DiagnosticObjectKey,
		report.IdempotencyHash, report.RequestHash, report.CapabilityHash, report.CreatedAt,
		report.UpdatedAt, report.RetentionUntil,
	)
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return ErrConflict
	}
	return err
}

func (postgres *Postgres) ByIdempotencyHash(ctx context.Context, hash []byte) (domain.Report, error) {
	return scanReport(postgres.pool.QueryRow(ctx, reportSelect+` WHERE idempotency_hash = $1 AND deleted_at IS NULL`, hash))
}

func (postgres *Postgres) ByCapabilityHash(ctx context.Context, hash []byte) (domain.Report, error) {
	return scanReport(postgres.pool.QueryRow(ctx, reportSelect+` WHERE capability_hash = $1 AND deleted_at IS NULL`, hash))
}

func (postgres *Postgres) DeleteByCapabilityHash(ctx context.Context, hash []byte) (domain.Report, error) {
	now := time.Now().UTC()
	return scanReport(postgres.pool.QueryRow(ctx, `UPDATE support_reports
		SET deleted_at = $1, updated_at = $1
		WHERE capability_hash = $2 AND deleted_at IS NULL
		RETURNING id, support_code, product_id, request_type, status, private_payload,
		capability_ciphertext, diagnostic_object_key, idempotency_hash, request_hash,
		capability_hash, created_at, updated_at, retention_until, deleted_at`, now, hash))
}

func (postgres *Postgres) Expired(ctx context.Context, before time.Time, limit int) ([]domain.Report, error) {
	rows, err := postgres.pool.Query(ctx, reportSelect+`
		WHERE deleted_at IS NOT NULL OR retention_until <= $1
		ORDER BY retention_until ASC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := make([]domain.Report, 0, limit)
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, rows.Err()
}

func (postgres *Postgres) Purge(ctx context.Context, id string, before time.Time) error {
	result, err := postgres.pool.Exec(ctx, `DELETE FROM support_reports
		WHERE id = $1 AND (deleted_at IS NOT NULL OR retention_until <= $2)`, id, before)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

const reportSelect = `SELECT id, support_code, product_id, request_type, status, private_payload,
	capability_ciphertext, diagnostic_object_key, idempotency_hash, request_hash,
	capability_hash, created_at, updated_at, retention_until, deleted_at FROM support_reports`

type rowScanner interface {
	Scan(...any) error
}

func scanReport(row rowScanner, transforms ...func(domain.Report) (domain.Report, error)) (domain.Report, error) {
	var report domain.Report
	if err := row.Scan(
		&report.ID, &report.SupportCode, &report.ProductID, &report.RequestType,
		&report.Status, &report.PrivatePayload, &report.CapabilityCiphertext,
		&report.DiagnosticObjectKey, &report.IdempotencyHash, &report.RequestHash,
		&report.CapabilityHash, &report.CreatedAt,
		&report.UpdatedAt, &report.RetentionUntil, &report.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Report{}, ErrNotFound
		}
		return domain.Report{}, err
	}
	for _, transform := range transforms {
		var err error
		report, err = transform(report)
		if err != nil {
			return domain.Report{}, err
		}
	}
	return report, nil
}

func (postgres *Postgres) Close() { postgres.pool.Close() }
