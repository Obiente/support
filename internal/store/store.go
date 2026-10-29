package store

import (
	"context"
	"errors"
	"time"

	"github.com/obiente/support/internal/domain"
)

// FirstReportID is the cursor that starts a DiagnosticReferences scan.
const FirstReportID = "00000000-0000-0000-0000-000000000000"

var (
	ErrNotFound  = errors.New("report not found")
	ErrConflict  = errors.New("report already exists")
	ErrCancelled = errors.New("report submission was cancelled")
)

type Reports interface {
	Create(context.Context, domain.Report) error
	ByIdempotencyHash(context.Context, []byte) (domain.Report, error)
	CancelByIdempotencyHash(context.Context, []byte, time.Time) (*domain.Report, error)
	ByCapabilityHash(context.Context, []byte) (domain.Report, error)
	DeleteByCapabilityHash(context.Context, []byte) (domain.Report, error)
	// Deleted lists reports already revoked by their reporter, a cancellation,
	// or a maintainer. Reports are never selected because of their age.
	Deleted(context.Context, int) ([]domain.Report, error)
	// PurgeDeleted hard-deletes one revoked report. It returns ErrNotFound for
	// a live or unknown report.
	PurgeDeleted(context.Context, string) error
	// DiagnosticReferences pages live reports with an attached diagnostic
	// object in ascending report ID order, starting after the given UUID.
	DiagnosticReferences(context.Context, string, int) ([]domain.DiagnosticReference, error)
	AdminList(context.Context, *domain.ReportStatus, int, int) ([]domain.Report, int, error)
	AdminByID(context.Context, string) (domain.Report, error)
	AdminUpdateStatus(context.Context, string, domain.ReportStatus, time.Time) (domain.Report, error)
	AdminDelete(context.Context, string, time.Time) (domain.Report, error)
	AddMessage(context.Context, domain.Message, *domain.ReportStatus, time.Time) (domain.Report, error)
	Messages(context.Context, string) ([]domain.Message, error)
	Close()
}

type AdminSessions interface {
	CreateAdminSession(context.Context, domain.AdminSession) error
	AdminSessionByHash(context.Context, []byte, time.Time) (domain.AdminSession, error)
	RotateAdminCSRF(context.Context, []byte, []byte) error
	DeleteAdminSession(context.Context, []byte) error
	DeleteExpiredAdminSessions(context.Context, time.Time) error
	RecordAdminAudit(context.Context, domain.AdminAudit) error
}
