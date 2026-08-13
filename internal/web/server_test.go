package web

import (
	"net/http"
	"net/http/httptest"
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
