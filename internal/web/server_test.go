package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersKeepPrivateRoutesOutOfIndexes(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))

	private := httptest.NewRecorder()
	handler.ServeHTTP(private, httptest.NewRequest(http.MethodGet, "/r/private-capability", nil))
	if got := private.Header().Get("X-Robots-Tag"); got != "noindex, nofollow, noarchive" {
		t.Fatalf("X-Robots-Tag = %q", got)
	}
	if got := private.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	if got := private.Header().Get("Permissions-Policy"); got == "" {
		t.Fatal("Permissions-Policy is empty")
	}

	public := httptest.NewRecorder()
	handler.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := public.Header().Get("X-Robots-Tag"); got != "" {
		t.Fatalf("public X-Robots-Tag = %q", got)
	}
}

func TestCancellationEndpointReturnsTerminalResult(t *testing.T) {
	handler, _, _ := testAdminHandler(t)
	idempotencyKey := strings.Repeat("A", 43)

	cancelRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/receipts", nil)
	cancelRequest.Header.Set("Idempotency-Key", idempotencyKey)
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, cancelRequest)
	if cancelled.Code != http.StatusNoContent || cancelled.Body.Len() != 0 {
		t.Fatalf("cancellation = %d, body = %q", cancelled.Code, cancelled.Body.String())
	}

	reconcileRequest := httptest.NewRequest(http.MethodGet, "/api/v1/receipts", nil)
	reconcileRequest.Header.Set("Idempotency-Key", idempotencyKey)
	reconciled := httptest.NewRecorder()
	handler.ServeHTTP(reconciled, reconcileRequest)
	if reconciled.Code != http.StatusGone || !strings.Contains(reconciled.Body.String(), "submission_cancelled") {
		t.Fatalf("reconciliation = %d, body = %q", reconciled.Code, reconciled.Body.String())
	}
}
