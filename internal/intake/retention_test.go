package intake

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/store"
)

func TestNewReportsHaveNoRetentionDeadline(t *testing.T) {
	service, reports, _ := testService(t)
	submission := validSubmission(t)
	receipt, err := service.Submit(context.Background(), submission)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := reports.ByIdempotencyHash(context.Background(), hash([]byte(submission.IdempotencyKey)))
	if err != nil {
		t.Fatal(err)
	}
	if stored.RetentionUntil != nil {
		t.Fatalf("stored retention deadline = %v, want none", stored.RetentionUntil)
	}
	if want := receipt.CreatedAt.Add(30 * 24 * time.Hour); !receipt.RetentionUntil.Equal(want) {
		t.Fatalf("contract v1 receipt link retention = %v, want %v", receipt.RetentionUntil, want)
	}
	detail, err := service.AdminDetail(context.Background(), stored.ID)
	if err != nil || !detail.RetentionUntil.Equal(receipt.RetentionUntil) {
		t.Fatalf("admin contract v1 retentionUntil = %v, %v; want %v", detail.RetentionUntil, err, receipt.RetentionUntil)
	}
}

func TestPurgeDeletedNeverPurgesOldLiveReports(t *testing.T) {
	service, reports, objects := testService(t)
	if _, err := service.Submit(context.Background(), validSubmission(t)); err != nil {
		t.Fatal(err)
	}
	longAgo := time.Date(2016, 1, 2, 3, 4, 5, 0, time.UTC)
	objectKey := strings.Repeat("ab", 32) + ".enc"
	objects.Values[objectKey] = []byte("legacy diagnostics")
	legacy := domain.Report{
		ID: "00000000-0000-4000-8000-000000000001", SupportCode: "OBI-LEGAC-Y2345", ProductID: "synthetic-product",
		RequestType: domain.RequestSupport, Status: domain.StatusResolved, DiagnosticObjectKey: &objectKey,
		IdempotencyHash: hash([]byte("legacy")), CapabilityHash: hash([]byte("legacy capability")),
		CreatedAt: longAgo, UpdatedAt: longAgo,
	}
	if err := reports.Create(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2046, 1, 1, 0, 0, 0, 0, time.UTC) }
	if err := service.PurgeDeleted(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if len(objects.Values) != 2 {
		t.Fatalf("private object count after purge = %d, want 2", len(objects.Values))
	}
	listed, total, err := reports.AdminList(context.Background(), nil, 10, 0)
	if err != nil || total != 2 || len(listed) != 2 {
		t.Fatalf("live reports after purge = %d (%d), %v; want 2", len(listed), total, err)
	}
}

func TestPurgeDeletedCompletesReporterDeletion(t *testing.T) {
	service, reports, objects := testService(t)
	receipt, err := service.Submit(context.Background(), validSubmission(t))
	if err != nil {
		t.Fatal(err)
	}
	capability := receipt.StatusURL[strings.LastIndex(receipt.StatusURL, "/")+1:]
	if _, err := service.Delete(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	pending, err := reports.Deleted(context.Background(), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("revoked rows before purge = %d, %v; want 1", len(pending), err)
	}
	if err := service.PurgeDeleted(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	pending, err = reports.Deleted(context.Background(), 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("revoked rows after purge = %d, %v; want 0", len(pending), err)
	}
	if len(objects.Values) != 0 {
		t.Fatalf("private object count after purge = %d, want 0", len(objects.Values))
	}
}

func TestPurgeDeletedKeepsRowWhenObjectDeletionFails(t *testing.T) {
	service, reports, objects := testService(t)
	if _, err := service.Submit(context.Background(), validSubmission(t)); err != nil {
		t.Fatal(err)
	}
	listed, _, err := service.AdminList(context.Background(), nil, 10, 0)
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed reports = %d, %v", len(listed), err)
	}
	service.objects = failingObjectDelete{objects}
	if err := service.AdminDelete(context.Background(), listed[0].ID); err == nil {
		t.Fatal("admin delete hid the object deletion failure")
	}
	if err := service.PurgeDeleted(context.Background(), 100); err == nil {
		t.Fatal("purge hid the object deletion failure")
	}
	pending, err := reports.Deleted(context.Background(), 10)
	if err != nil || len(pending) != 1 || pending[0].DiagnosticObjectKey == nil {
		t.Fatalf("revoked rows after failed purge = %d, %v; want the row and its object key", len(pending), err)
	}
	if len(objects.Values) != 1 {
		t.Fatalf("private object count after failed purge = %d, want 1", len(objects.Values))
	}

	service.objects = objects
	if err := service.PurgeDeleted(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if pending, err = reports.Deleted(context.Background(), 10); err != nil || len(pending) != 0 || len(objects.Values) != 0 {
		t.Fatalf("retry left %d rows and %d objects, %v", len(pending), len(objects.Values), err)
	}
}

func TestAdminDeleteRevokesReportAndRemovesDiagnosticObject(t *testing.T) {
	service, _, objects := testService(t)
	receipt, err := service.Submit(context.Background(), validSubmission(t))
	if err != nil {
		t.Fatal(err)
	}
	listed, _, err := service.AdminList(context.Background(), nil, 10, 0)
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed reports = %d, %v", len(listed), err)
	}
	if err := service.AdminDelete(context.Background(), listed[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(objects.Values) != 0 {
		t.Fatalf("private object count after admin deletion = %d", len(objects.Values))
	}
	if _, err := service.AdminDetail(context.Background(), listed[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("admin detail after deletion = %v, want ErrNotFound", err)
	}
	capability := receipt.StatusURL[strings.LastIndex(receipt.StatusURL, "/")+1:]
	if _, err := service.Status(context.Background(), capability); !errors.Is(err, ErrNotFound) {
		t.Fatalf("capability status after admin deletion = %v, want ErrNotFound", err)
	}
	if err := service.AdminDelete(context.Background(), listed[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repeated admin deletion = %v, want ErrNotFound", err)
	}
	if err := service.AdminDelete(context.Background(), "00000000-0000-4000-8000-00000000abcd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown admin deletion = %v, want ErrNotFound", err)
	}
}

func TestInventoryDiagnosticsCountsMissingObjectsWithoutChangingReports(t *testing.T) {
	service, _, objects := testService(t)
	second := validSubmission(t)
	second.IdempotencyKey = strings.Repeat("C", 43)
	second.Metadata.Title = "Second synthetic failure"
	withoutArchive := validSubmission(t)
	withoutArchive.IdempotencyKey = strings.Repeat("D", 43)
	withoutArchive.Archive = nil
	withoutArchive.ArchiveType = ""
	for _, submission := range []Submission{validSubmission(t), second, withoutArchive} {
		if _, err := service.Submit(context.Background(), submission); err != nil {
			t.Fatal(err)
		}
	}
	for key := range objects.Values {
		delete(objects.Values, key)
		break
	}
	inventory, err := service.InventoryDiagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inventory != (DiagnosticInventory{Attached: 2, Missing: 1}) {
		t.Fatalf("inventory = %#v, want 2 attached and 1 missing", inventory)
	}
	listed, total, err := service.AdminList(context.Background(), nil, 10, 0)
	if err != nil || total != 3 {
		t.Fatalf("reports after inventory = %d, %v; want 3", total, err)
	}
	for _, report := range listed {
		if report.Status != domain.StatusNew {
			t.Fatalf("inventory changed report %s status to %s", report.SupportCode, report.Status)
		}
	}
}

type failingObjectDelete struct{ *store.MemoryObjects }

func (failingObjectDelete) Delete(string) error {
	return errors.New("synthetic object deletion failure")
}

func TestInventoryDiagnosticsCountsOrphanedObjectsWithoutDeletingThem(t *testing.T) {
	service, _, objects := testService(t)
	if _, err := service.Submit(context.Background(), validSubmission(t)); err != nil {
		t.Fatal(err)
	}
	pendingDeletion := validSubmission(t)
	pendingDeletion.IdempotencyKey = strings.Repeat("E", 43)
	pendingDeletion.Metadata.Title = "Revoked synthetic failure"
	if _, err := service.Submit(context.Background(), pendingDeletion); err != nil {
		t.Fatal(err)
	}
	listed, _, err := service.AdminList(context.Background(), nil, 10, 0)
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed reports = %d, %v", len(listed), err)
	}
	service.objects = failingObjectDelete{objects}
	if err := service.AdminDelete(context.Background(), listed[0].ID); err == nil {
		t.Fatal("admin delete hid the object deletion failure")
	}
	service.objects = objects
	orphans := []string{strings.Repeat("0f", 32) + ".enc", strings.Repeat("f0", 32) + ".enc"}
	for _, key := range orphans {
		objects.Values[key] = []byte("sealed for a purged report")
	}

	inventory, err := service.InventoryDiagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inventory != (DiagnosticInventory{Attached: 1, Orphaned: 2}) {
		t.Fatalf("inventory = %#v, want 1 attached and 2 orphaned (the revoked report's object is still referenced)", inventory)
	}
	if len(objects.Values) != 4 {
		t.Fatalf("private object count after inventory = %d, want 4", len(objects.Values))
	}
}
