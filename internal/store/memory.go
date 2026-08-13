package store

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/obiente/support/internal/domain"
)

type Memory struct {
	mu      sync.Mutex
	reports []domain.Report
}

func NewMemory() *Memory { return &Memory{} }

func (memory *Memory) Create(_ context.Context, report domain.Report) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for _, existing := range memory.reports {
		if existing.SupportCode == report.SupportCode || bytes.Equal(existing.IdempotencyHash, report.IdempotencyHash) {
			return ErrConflict
		}
	}
	memory.reports = append(memory.reports, report)
	return nil
}

func (memory *Memory) ByIdempotencyHash(_ context.Context, hash []byte) (domain.Report, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for _, report := range memory.reports {
		if bytes.Equal(report.IdempotencyHash, hash) && report.DeletedAt == nil {
			return report, nil
		}
	}
	return domain.Report{}, ErrNotFound
}

func (memory *Memory) ByCapabilityHash(_ context.Context, hash []byte) (domain.Report, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for _, report := range memory.reports {
		if bytes.Equal(report.CapabilityHash, hash) && report.DeletedAt == nil {
			return report, nil
		}
	}
	return domain.Report{}, ErrNotFound
}

func (memory *Memory) DeleteByCapabilityHash(_ context.Context, hash []byte) (domain.Report, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for index, report := range memory.reports {
		if bytes.Equal(report.CapabilityHash, hash) && report.DeletedAt == nil {
			now := time.Now().UTC()
			memory.reports[index].DeletedAt = &now
			memory.reports[index].UpdatedAt = now
			return memory.reports[index], nil
		}
	}
	return domain.Report{}, ErrNotFound
}

func (memory *Memory) Expired(_ context.Context, before time.Time, limit int) ([]domain.Report, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	result := make([]domain.Report, 0, limit)
	for _, report := range memory.reports {
		if report.DeletedAt != nil || !report.RetentionUntil.After(before) {
			result = append(result, report)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (memory *Memory) Purge(_ context.Context, id string, before time.Time) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for index, report := range memory.reports {
		if report.ID == id && (report.DeletedAt != nil || !report.RetentionUntil.After(before)) {
			memory.reports = append(memory.reports[:index], memory.reports[index+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (memory *Memory) Close() {}
