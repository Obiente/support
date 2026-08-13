package store

import (
	"context"
	"errors"
	"time"

	"github.com/obiente/support/internal/domain"
)

var (
	ErrNotFound = errors.New("report not found")
	ErrConflict = errors.New("report already exists")
)

type Reports interface {
	Create(context.Context, domain.Report) error
	ByIdempotencyHash(context.Context, []byte) (domain.Report, error)
	ByCapabilityHash(context.Context, []byte) (domain.Report, error)
	DeleteByCapabilityHash(context.Context, []byte) (domain.Report, error)
	Expired(context.Context, time.Time, int) ([]domain.Report, error)
	Purge(context.Context, string, time.Time) error
	AdminList(context.Context, *domain.ReportStatus, int, int) ([]domain.Report, int, error)
	AdminByID(context.Context, string) (domain.Report, error)
	AdminUpdateStatus(context.Context, string, domain.ReportStatus, time.Time) (domain.Report, error)
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
