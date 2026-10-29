package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obiente/support/internal/cryptobox"
	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/intake"
	"github.com/obiente/support/internal/products"
	"github.com/obiente/support/internal/store"
)

func TestOpenPrivateObjectsRefusesUnwritableRoot(t *testing.T) {
	box := testBox(t)
	file := filepath.Join(t.TempDir(), "volume-missing")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openPrivateObjects(filepath.Join(file, "private"), box); err == nil ||
		!strings.Contains(err.Error(), "refusing to start") {
		t.Fatalf("open below a regular file = %v, want a startup refusal", err)
	}
	if _, err := openPrivateObjects(filepath.Join(t.TempDir(), "private"), box); err != nil {
		t.Fatalf("open writable root: %v", err)
	}
}

func TestDiagnosticInventoryWarnsAboutMissingObjectsWithoutDeletingReports(t *testing.T) {
	service, reports, objects := testIntake(t)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))

	reportDiagnosticInventory(context.Background(), logger, "/data/private", service)
	entry := lastLogEntry(t, &output)
	if entry["level"] != "INFO" || entry["missing"] != float64(0) || entry["attached"] != float64(1) {
		t.Fatalf("healthy inventory log = %v", entry)
	}

	for key := range objects.Values {
		delete(objects.Values, key)
	}
	output.Reset()
	reportDiagnosticInventory(context.Background(), logger, "/data/private", service)
	entry = lastLogEntry(t, &output)
	if entry["level"] != "WARN" || entry["object_root"] != "/data/private" || entry["missing"] != float64(1) {
		t.Fatalf("missing-object inventory log = %v", entry)
	}
	if _, total, err := reports.AdminList(context.Background(), nil, 10, 0); err != nil || total != 1 {
		t.Fatalf("reports after inventory = %d, %v; want 1", total, err)
	}
}

func lastLogEntry(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("log entry %q: %v", lines[len(lines)-1], err)
	}
	return entry
}

func testBox(t *testing.T) *cryptobox.Box {
	t.Helper()
	box, err := cryptobox.NewFromBase64(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func testIntake(t *testing.T) (*intake.Service, *store.Memory, *store.MemoryObjects) {
	t.Helper()
	registry, err := products.New([]products.Product{{
		ID: "synthetic-product", Name: "Synthetic product",
		DiagnosticContentTypes: []string{"application/zip"}, DiagnosticEntries: []string{"diagnostic.txt"},
		DiagnosticMaxBytes: 4096, DiagnosticMaxExpanded: 2048,
	}})
	if err != nil {
		t.Fatal(err)
	}
	reports := store.NewMemory()
	objects := store.NewMemoryObjects()
	service := intake.New(reports, objects, registry, testBox(t), "https://support.example")
	if _, err := service.Submit(context.Background(), intake.Submission{
		Metadata: domain.ReportMetadata{
			ContractVersion: 1, ProductID: "synthetic-product", RequestType: domain.RequestBug,
			Title: "Synthetic failure", Description: "The synthetic action did not complete.", Source: "app",
			Release: domain.ReleaseMetadata{Version: "1.0.0", Platform: "linux"}, PrivacyAccepted: true,
		},
		Archive: diagnosticArchive(t), ArchiveType: "application/zip", IdempotencyKey: strings.Repeat("A", 43),
	}); err != nil {
		t.Fatal(err)
	}
	return service, reports, objects
}

func diagnosticArchive(t *testing.T) []byte {
	t.Helper()
	var content bytes.Buffer
	writer := zip.NewWriter(&content)
	entry, err := writer.Create("diagnostic.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("bounded diagnostics")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return content.Bytes()
}
