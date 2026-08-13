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
	Close()
}
