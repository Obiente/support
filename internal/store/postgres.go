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
	transaction, err := postgres.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Exec(ctx, `INSERT INTO support_submission_states (idempotency_hash)
		VALUES ($1) ON CONFLICT (idempotency_hash) DO NOTHING`, report.IdempotencyHash); err != nil {
		return err
	}
	var cancelledAt *time.Time
	if err := transaction.QueryRow(ctx, `SELECT cancelled_at FROM support_submission_states
		WHERE idempotency_hash = $1 FOR UPDATE`, report.IdempotencyHash).Scan(&cancelledAt); err != nil {
		return err
	}
	if cancelledAt != nil {
		return ErrCancelled
	}
	_, err = transaction.Exec(ctx, `
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
	if err != nil {
		return err
	}
	return transaction.Commit(ctx)
}

func (postgres *Postgres) ByIdempotencyHash(ctx context.Context, hash []byte) (domain.Report, error) {
	report, err := scanReport(postgres.pool.QueryRow(ctx, reportSelect+` WHERE idempotency_hash = $1 AND deleted_at IS NULL`, hash))
	if !errors.Is(err, ErrNotFound) {
		return report, err
	}
	var cancelled bool
	if lookupErr := postgres.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM support_submission_states WHERE idempotency_hash = $1 AND cancelled_at IS NOT NULL
	)`, hash).Scan(&cancelled); lookupErr != nil {
		return domain.Report{}, lookupErr
	}
	if cancelled {
		return domain.Report{}, ErrCancelled
	}
	return domain.Report{}, ErrNotFound
}

func (postgres *Postgres) CancelByIdempotencyHash(ctx context.Context, hash []byte, now time.Time) (*domain.Report, error) {
	transaction, err := postgres.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer transaction.Rollback(ctx)
	if _, err := transaction.Exec(ctx, `INSERT INTO support_submission_states (idempotency_hash, cancelled_at)
		VALUES ($1, $2)
		ON CONFLICT (idempotency_hash) DO UPDATE
		SET cancelled_at = COALESCE(support_submission_states.cancelled_at, EXCLUDED.cancelled_at)`, hash, now); err != nil {
		return nil, err
	}
	report, updateErr := scanReport(transaction.QueryRow(ctx, `UPDATE support_reports
		SET deleted_at = COALESCE(deleted_at, $1),
			updated_at = CASE WHEN deleted_at IS NULL THEN $1 ELSE updated_at END
		WHERE idempotency_hash = $2
		RETURNING id, support_code, product_id, request_type, status, private_payload,
		capability_ciphertext, diagnostic_object_key, idempotency_hash, request_hash,
		capability_hash, created_at, updated_at, retention_until, deleted_at`, now, hash))
	if updateErr != nil && !errors.Is(updateErr, ErrNotFound) {
		return nil, updateErr
	}
	if err := transaction.Commit(ctx); err != nil {
		return nil, err
	}
	if errors.Is(updateErr, ErrNotFound) {
		return nil, nil
	}
	return &report, nil
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

func (postgres *Postgres) AdminList(ctx context.Context, status *domain.ReportStatus, limit, offset int) ([]domain.Report, int, error) {
	var statusValue any
	if status != nil {
		statusValue = string(*status)
	}
	var total int
	if err := postgres.pool.QueryRow(ctx, `SELECT count(*) FROM support_reports
		WHERE deleted_at IS NULL AND ($1::text IS NULL OR status = $1)`, statusValue).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := postgres.pool.Query(ctx, reportSelect+`
		WHERE deleted_at IS NULL AND ($1::text IS NULL OR status = $1)
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, statusValue, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	reports := make([]domain.Report, 0, limit)
	for rows.Next() {
		report, scanErr := scanReport(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		reports = append(reports, report)
	}
	return reports, total, rows.Err()
}

func (postgres *Postgres) AdminByID(ctx context.Context, id string) (domain.Report, error) {
	return scanReport(postgres.pool.QueryRow(ctx, reportSelect+` WHERE id = $1 AND deleted_at IS NULL`, id))
}

func (postgres *Postgres) AdminUpdateStatus(ctx context.Context, id string, status domain.ReportStatus, now time.Time) (domain.Report, error) {
	return scanReport(postgres.pool.QueryRow(ctx, `UPDATE support_reports SET status = $1, updated_at = $2
		WHERE id = $3 AND deleted_at IS NULL
		RETURNING id, support_code, product_id, request_type, status, private_payload,
		capability_ciphertext, diagnostic_object_key, idempotency_hash, request_hash,
		capability_hash, created_at, updated_at, retention_until, deleted_at`, status, now, id))
}

func (postgres *Postgres) CreateAdminSession(ctx context.Context, session domain.AdminSession) error {
	_, err := postgres.pool.Exec(ctx, `INSERT INTO support_admin_sessions
		(token_hash, csrf_hash, username, created_at, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		session.TokenHash, session.CSRFHash, session.Username, session.CreatedAt, session.ExpiresAt)
	return err
}

func (postgres *Postgres) AdminSessionByHash(ctx context.Context, tokenHash []byte, now time.Time) (domain.AdminSession, error) {
	var session domain.AdminSession
	err := postgres.pool.QueryRow(ctx, `SELECT token_hash, csrf_hash, username, created_at, expires_at
		FROM support_admin_sessions WHERE token_hash = $1 AND expires_at > $2`, tokenHash, now).Scan(
		&session.TokenHash, &session.CSRFHash, &session.Username, &session.CreatedAt, &session.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AdminSession{}, ErrNotFound
	}
	return session, err
}

func (postgres *Postgres) RotateAdminCSRF(ctx context.Context, tokenHash, csrfHash []byte) error {
	result, err := postgres.pool.Exec(ctx, `UPDATE support_admin_sessions SET csrf_hash = $1 WHERE token_hash = $2`, csrfHash, tokenHash)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (postgres *Postgres) DeleteAdminSession(ctx context.Context, tokenHash []byte) error {
	_, err := postgres.pool.Exec(ctx, `DELETE FROM support_admin_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (postgres *Postgres) DeleteExpiredAdminSessions(ctx context.Context, now time.Time) error {
	_, err := postgres.pool.Exec(ctx, `DELETE FROM support_admin_sessions WHERE expires_at <= $1`, now)
	return err
}

func (postgres *Postgres) RecordAdminAudit(ctx context.Context, audit domain.AdminAudit) error {
	_, err := postgres.pool.Exec(ctx, `INSERT INTO support_admin_audit
		(username, action, report_id, remote_hash, created_at) VALUES ($1,$2,$3,$4,$5)`,
		audit.Username, audit.Action, audit.ReportID, audit.RemoteHash, audit.CreatedAt)
	return err
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
