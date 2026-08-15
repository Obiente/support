package store

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/obiente/support/internal/domain"
)

type Memory struct {
	mu            sync.Mutex
	reports       []domain.Report
	cancellations map[string]time.Time
	sessions      []domain.AdminSession
}

func NewMemory() *Memory { return &Memory{cancellations: make(map[string]time.Time)} }

func (memory *Memory) Create(_ context.Context, report domain.Report) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if _, cancelled := memory.cancellations[string(report.IdempotencyHash)]; cancelled {
		return ErrCancelled
	}
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
	if _, cancelled := memory.cancellations[string(hash)]; cancelled {
		return domain.Report{}, ErrCancelled
	}
	return domain.Report{}, ErrNotFound
}

func (memory *Memory) CancelByIdempotencyHash(_ context.Context, hash []byte, now time.Time) (*domain.Report, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	memory.cancellations[string(hash)] = now
	for index, report := range memory.reports {
		if bytes.Equal(report.IdempotencyHash, hash) {
			if report.DeletedAt == nil {
				memory.reports[index].DeletedAt = &now
				memory.reports[index].UpdatedAt = now
			}
			cancelled := memory.reports[index]
			return &cancelled, nil
		}
	}
	return nil, nil
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

func (memory *Memory) AdminList(_ context.Context, status *domain.ReportStatus, limit, offset int) ([]domain.Report, int, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	filtered := make([]domain.Report, 0, len(memory.reports))
	for index := len(memory.reports) - 1; index >= 0; index-- {
		report := memory.reports[index]
		if report.DeletedAt == nil && (status == nil || report.Status == *status) {
			filtered = append(filtered, report)
		}
	}
	total := len(filtered)
	if offset >= total {
		return []domain.Report{}, total, nil
	}
	end := min(offset+limit, total)
	return append([]domain.Report(nil), filtered[offset:end]...), total, nil
}

func (memory *Memory) AdminByID(_ context.Context, id string) (domain.Report, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for _, report := range memory.reports {
		if report.ID == id && report.DeletedAt == nil {
			return report, nil
		}
	}
	return domain.Report{}, ErrNotFound
}

func (memory *Memory) AdminUpdateStatus(_ context.Context, id string, status domain.ReportStatus, now time.Time) (domain.Report, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for index, report := range memory.reports {
		if report.ID == id && report.DeletedAt == nil {
			memory.reports[index].Status = status
			memory.reports[index].UpdatedAt = now
			return memory.reports[index], nil
		}
	}
	return domain.Report{}, ErrNotFound
}

func (memory *Memory) CreateAdminSession(_ context.Context, session domain.AdminSession) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	memory.sessions = append(memory.sessions, session)
	return nil
}

func (memory *Memory) AdminSessionByHash(_ context.Context, tokenHash []byte, now time.Time) (domain.AdminSession, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for _, session := range memory.sessions {
		if bytes.Equal(session.TokenHash, tokenHash) && session.ExpiresAt.After(now) {
			return session, nil
		}
	}
	return domain.AdminSession{}, ErrNotFound
}

func (memory *Memory) RotateAdminCSRF(_ context.Context, tokenHash, csrfHash []byte) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for index := range memory.sessions {
		if bytes.Equal(memory.sessions[index].TokenHash, tokenHash) {
			memory.sessions[index].CSRFHash = append([]byte(nil), csrfHash...)
			return nil
		}
	}
	return ErrNotFound
}

func (memory *Memory) DeleteAdminSession(_ context.Context, tokenHash []byte) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for index := range memory.sessions {
		if bytes.Equal(memory.sessions[index].TokenHash, tokenHash) {
			memory.sessions = append(memory.sessions[:index], memory.sessions[index+1:]...)
			return nil
		}
	}
	return nil
}

func (memory *Memory) DeleteExpiredAdminSessions(_ context.Context, now time.Time) error {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	kept := memory.sessions[:0]
	for _, session := range memory.sessions {
		if session.ExpiresAt.After(now) {
			kept = append(kept, session)
		}
	}
	memory.sessions = kept
	return nil
}

func (memory *Memory) RecordAdminAudit(_ context.Context, _ domain.AdminAudit) error { return nil }

func (memory *Memory) Close() {}
