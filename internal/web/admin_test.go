package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obiente/support/internal/admin"
	"github.com/obiente/support/internal/cryptobox"
	"github.com/obiente/support/internal/domain"
	"github.com/obiente/support/internal/intake"
	"github.com/obiente/support/internal/products"
	"github.com/obiente/support/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func TestAdminSessionProtectsReviewAndDiagnosticRoutes(t *testing.T) {
	handler, reportID := testAdminHandler(t)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized list status = %d", unauthorized.Code)
	}

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/v1/admin/login",
		strings.NewReader(`{"username":"maintainer","password":"test password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("admin cookie = %#v", cookies)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports", nil)
	listRequest.AddCookie(cookies[0])
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, listRequest)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "Synthetic failure") {
		t.Fatalf("list status = %d, body = %s", listed.Code, listed.Body.String())
	}

	withoutCSRF := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/reports/"+reportID, strings.NewReader(`{"status":"accepted"}`))
	withoutCSRF.AddCookie(cookies[0])
	forbidden := httptest.NewRecorder()
	handler.ServeHTTP(forbidden, withoutCSRF)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("status update without csrf = %d", forbidden.Code)
	}

	update := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/reports/"+reportID, strings.NewReader(`{"status":"accepted"}`))
	update.AddCookie(cookies[0])
	update.Header.Set("X-CSRF-Token", session.CSRFToken)
	updated := httptest.NewRecorder()
	handler.ServeHTTP(updated, update)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"status":"accepted"`) {
		t.Fatalf("status update = %d, body = %s", updated.Code, updated.Body.String())
	}

	download := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/"+reportID+"/diagnostics", nil)
	download.AddCookie(cookies[0])
	diagnostics := httptest.NewRecorder()
	handler.ServeHTTP(diagnostics, download)
	if diagnostics.Code != http.StatusOK || diagnostics.Header().Get("Content-Type") != "application/zip" || diagnostics.Body.Len() == 0 {
		t.Fatalf("diagnostics = %d, %q, %d bytes", diagnostics.Code, diagnostics.Header().Get("Content-Type"), diagnostics.Body.Len())
	}
}

func testAdminHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	box, err := cryptobox.NewFromBase64(base64.StdEncoding.EncodeToString(make([]byte, 32)))
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
	intakeService := intake.New(reports, objects, registry, box, "https://support.example")
	receipt, err := intakeService.Submit(context.Background(), intake.Submission{
		Metadata: domain.ReportMetadata{
			ContractVersion: 1, ProductID: "synthetic-product", RequestType: domain.RequestBug,
			Title: "Synthetic failure", Description: "The synthetic action did not complete.", Source: "app",
			Release: domain.ReleaseMetadata{Version: "1.0.0", Platform: "linux"}, PrivacyAccepted: true,
		},
		Archive: testDiagnosticArchive(t), ArchiveType: "application/zip", IdempotencyKey: strings.Repeat("A", 43),
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, _, err := intakeService.AdminList(context.Background(), nil, 10, 0)
	if err != nil || len(listed) != 1 || listed[0].SupportCode != receipt.SupportCode {
		t.Fatalf("seeded report = %#v, %v", listed, err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("test password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	adminService := admin.New(reports, "maintainer", string(passwordHash))
	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<!doctype html><title>Support</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(intakeService, adminService, registry, slog.New(slog.NewTextHandler(io.Discard, nil)), webRoot, true)
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler(), listed[0].ID
}

func testDiagnosticArchive(t *testing.T) []byte {
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
