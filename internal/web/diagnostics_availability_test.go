package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/obiente/support/internal/intake"
)

func TestMissingDiagnosticArchiveIsNotAMissingReport(t *testing.T) {
	server := &Server{}
	response := httptest.NewRecorder()
	server.writeAdminError(response, intake.ErrDiagnosticsUnavailable)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "diagnostics_unavailable") {
		t.Fatalf("unexpected response: %d", response.Code)
	}
	response = httptest.NewRecorder()
	server.writeAdminError(response, intake.ErrNotFound)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing report status: %d", response.Code)
	}
}
