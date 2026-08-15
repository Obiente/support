package intake

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/obiente/support/internal/cryptobox"
	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/products"
	"github.com/obiente/support/internal/store"
)

func TestSubmitReconcilesWithoutDuplicatingPrivateReport(t *testing.T) {
	service, reports, objects := testService(t)
	submission := validSubmission(t)
	first, err := service.Submit(context.Background(), submission)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Submit(context.Background(), submission)
	if err != nil {
		t.Fatal(err)
	}
	reconciled, err := service.Reconcile(context.Background(), submission.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	if first.SupportCode != second.SupportCode || first.StatusURL != reconciled.StatusURL {
		t.Fatalf("receipts differ: first=%#v second=%#v reconciled=%#v", first, second, reconciled)
	}
	if len(objects.Values) != 1 {
		t.Fatalf("private object count = %d, want 1", len(objects.Values))
	}
	stored, err := reports.ByIdempotencyHash(context.Background(), hash([]byte(submission.IdempotencyKey)))
	if err != nil || stored.DiagnosticObjectKey == nil {
		t.Fatalf("stored report = %#v, %v", stored, err)
	}
}

func TestSubmitRejectsIdempotencyKeyReuseWithDifferentContent(t *testing.T) {
	service, _, _ := testService(t)
	submission := validSubmission(t)
	if _, err := service.Submit(context.Background(), submission); err != nil {
		t.Fatal(err)
	}
	submission.Metadata.Description = "A materially different support request."
	if _, err := service.Submit(context.Background(), submission); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("error = %v, want ErrKeyReused", err)
	}
}

func TestCancelBeforeSubmitPreventsLatePrivateReport(t *testing.T) {
	service, reports, objects := testService(t)
	submission := validSubmission(t)
	if err := service.Cancel(context.Background(), submission.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(context.Background(), submission.IdempotencyKey); err != nil {
		t.Fatalf("repeated cancel: %v", err)
	}
	if _, err := service.Submit(context.Background(), submission); !errors.Is(err, ErrCancelled) {
		t.Fatalf("late submit error = %v, want ErrCancelled", err)
	}
	if _, err := service.Reconcile(context.Background(), submission.IdempotencyKey); !errors.Is(err, ErrCancelled) {
		t.Fatalf("reconcile error = %v, want ErrCancelled", err)
	}
	if _, err := reports.ByIdempotencyHash(context.Background(), hash([]byte(submission.IdempotencyKey))); !errors.Is(err, store.ErrCancelled) {
		t.Fatalf("stored cancellation error = %v, want ErrCancelled", err)
	}
	if len(objects.Values) != 0 {
		t.Fatalf("private object count = %d, want 0", len(objects.Values))
	}
}

func TestCancelExistingReportDeletesPrivateDataAndPreventsRecreation(t *testing.T) {
	service, reports, objects := testService(t)
	submission := validSubmission(t)
	if _, err := service.Submit(context.Background(), submission); err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(context.Background(), submission.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if err := service.Cancel(context.Background(), submission.IdempotencyKey); err != nil {
		t.Fatalf("repeated cancel: %v", err)
	}
	if len(objects.Values) != 0 {
		t.Fatalf("private object count after cancellation = %d, want 0", len(objects.Values))
	}
	if _, err := reports.ByIdempotencyHash(context.Background(), hash([]byte(submission.IdempotencyKey))); !errors.Is(err, store.ErrCancelled) {
		t.Fatalf("stored cancellation error = %v, want ErrCancelled", err)
	}
	if _, err := service.Submit(context.Background(), submission); !errors.Is(err, ErrCancelled) {
		t.Fatalf("recreated submit error = %v, want ErrCancelled", err)
	}
}

func TestSubmitRejectsUnregisteredAndExpandingArchiveEntries(t *testing.T) {
	service, _, _ := testService(t)
	submission := validSubmission(t)
	submission.Archive = zipContent(t, map[string]string{"../private.txt": "secret"})
	if _, err := service.Submit(context.Background(), submission); !errors.Is(err, ErrInvalid) {
		t.Fatalf("traversal error = %v, want ErrInvalid", err)
	}

	submission.IdempotencyKey = strings.Repeat("B", 43)
	submission.Archive = zipContent(t, map[string]string{"diagnostic.txt": strings.Repeat("x", 2049)})
	if _, err := service.Submit(context.Background(), submission); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expansion error = %v, want ErrInvalid", err)
	}
}

func TestDeleteRevokesCapabilityAndRemovesDiagnosticObject(t *testing.T) {
	service, _, objects := testService(t)
	receipt, err := service.Submit(context.Background(), validSubmission(t))
	if err != nil {
		t.Fatal(err)
	}
	capability := receipt.StatusURL[strings.LastIndex(receipt.StatusURL, "/")+1:]
	if _, err := service.Status(context.Background(), capability); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.Delete(context.Background(), capability)
	if err != nil || deleted.Status != "deleted" {
		t.Fatalf("delete = %#v, %v", deleted, err)
	}
	if len(objects.Values) != 0 {
		t.Fatalf("private object count after deletion = %d", len(objects.Values))
	}
	if _, err := service.Status(context.Background(), capability); !errors.Is(err, ErrNotFound) {
		t.Fatalf("status error after deletion = %v, want ErrNotFound", err)
	}
}

func TestPurgeExpiredRemovesEncryptedObjectAndRow(t *testing.T) {
	service, reports, objects := testService(t)
	submission := validSubmission(t)
	if _, err := service.Submit(context.Background(), submission); err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }
	if err := service.PurgeExpired(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if len(objects.Values) != 0 {
		t.Fatalf("private object count after retention = %d", len(objects.Values))
	}
	if _, err := reports.ByIdempotencyHash(context.Background(), hash([]byte(submission.IdempotencyKey))); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retained row error = %v, want ErrNotFound", err)
	}
}

func TestAdminReviewDecryptsOnlyRequestedPrivateData(t *testing.T) {
	service, _, _ := testService(t)
	receipt, err := service.Submit(context.Background(), validSubmission(t))
	if err != nil {
		t.Fatal(err)
	}
	reports, total, err := service.AdminList(context.Background(), nil, 25, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(reports) != 1 || reports[0].SupportCode != receipt.SupportCode || reports[0].Source != "app" {
		t.Fatalf("admin list = %#v, total %d", reports, total)
	}
	detail, err := service.AdminDetail(context.Background(), reports[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Description != "The synthetic action did not complete." || !detail.HasDiagnostics {
		t.Fatalf("admin detail = %#v", detail)
	}
	archive, filename, err := service.AdminDiagnostics(context.Background(), reports[0].ID)
	if err != nil || len(archive) == 0 || filename != receipt.SupportCode+"-diagnostics.zip" {
		t.Fatalf("admin diagnostics = %q bytes, %q, %v", len(archive), filename, err)
	}
	updated, err := service.AdminUpdateStatus(context.Background(), reports[0].ID, domain.StatusAccepted)
	if err != nil || updated.Status != domain.StatusAccepted {
		t.Fatalf("admin update = %#v, %v", updated, err)
	}
}

func testService(t *testing.T) (*Service, *store.Memory, *store.MemoryObjects) {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	box, err := cryptobox.NewFromBase64(key)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := products.New([]products.Product{{
		ID: "synthetic-product", Name: "Synthetic product", RetentionDays: 7,
		DiagnosticContentTypes: []string{"application/zip"}, DiagnosticEntries: []string{"diagnostic.txt"},
		DiagnosticMaxBytes: 4096, DiagnosticMaxExpanded: 2048,
	}})
	if err != nil {
		t.Fatal(err)
	}
	reports := store.NewMemory()
	objects := store.NewMemoryObjects()
	service := New(reports, objects, registry, box, "https://support.example")
	service.now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }
	return service, reports, objects
}

func validSubmission(t *testing.T) Submission {
	t.Helper()
	return Submission{
		Metadata: domain.ReportMetadata{
			ContractVersion: 1, ProductID: "synthetic-product", RequestType: domain.RequestBug,
			Title: "Synthetic failure", Description: "The synthetic action did not complete.",
			Source: "app", Release: domain.ReleaseMetadata{Version: "1.0.0", Platform: "synthetic"},
			PrivacyAccepted: true,
		},
		Archive:     zipContent(t, map[string]string{"diagnostic.txt": "bounded diagnostics"}),
		ArchiveType: "application/zip", IdempotencyKey: strings.Repeat("A", 43),
	}
}

func zipContent(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var content bytes.Buffer
	writer := zip.NewWriter(&content)
	for name, value := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return content.Bytes()
}
