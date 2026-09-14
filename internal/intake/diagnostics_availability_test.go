package intake

import (
	"context"
	"errors"
	"testing"

	"github.com/obiente/support/internal/store"
)

func TestMissingDiagnosticObjectPreservesReportAndAdvertisesUnavailable(t *testing.T) {
	service, _, objects := testService(t)
	if _, err := service.Submit(context.Background(), validSubmission(t)); err != nil {
		t.Fatal(err)
	}
	reports, _, err := service.AdminList(context.Background(), nil, 25, 0)
	if err != nil || len(reports) != 1 {
		t.Fatalf("list: %v", err)
	}
	id := reports[0].ID
	if reports[0].DiagnosticsState != "available" {
		t.Fatal("attached archive is not available")
	}
	for key := range objects.Values {
		delete(objects.Values, key)
	}
	detail, err := service.AdminDetail(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.HasDiagnostics || detail.DiagnosticsState != "unavailable" {
		t.Fatal("missing attachment state was lost")
	}
	if _, _, err := service.AdminDiagnostics(context.Background(), id); !errors.Is(err, ErrDiagnosticsUnavailable) {
		t.Fatalf("download error: %v", err)
	}
	reports, _, err = service.AdminList(context.Background(), nil, 25, 0)
	if err != nil || reports[0].DiagnosticsState != "unavailable" {
		t.Fatal("list did not retain unavailable report")
	}
}

func TestObjectStorageFailureIsUnknownNotMissing(t *testing.T) {
	service, _, objects := testService(t)
	if _, err := service.Submit(context.Background(), validSubmission(t)); err != nil {
		t.Fatal(err)
	}
	service.objects = failingObjectProbe{objects}
	reports, _, err := service.AdminList(context.Background(), nil, 25, 0)
	if err != nil || reports[0].DiagnosticsState != "unknown" {
		t.Fatal("storage outage should preserve report metadata")
	}
}

type failingObjectProbe struct{ *store.MemoryObjects }

func (failingObjectProbe) Exists(string) (bool, error) {
	return false, errors.New("synthetic storage outage")
}
