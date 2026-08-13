package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheckURLUsesRuntimeAddress(t *testing.T) {
	tests := map[string]string{
		"":                   "http://127.0.0.1:8080/healthz",
		":9090":              "http://127.0.0.1:9090/healthz",
		"0.0.0.0:8081":       "http://127.0.0.1:8081/healthz",
		"[::]:8082":          "http://127.0.0.1:8082/healthz",
		"[::1]:8083":         "http://[::1]:8083/healthz",
		"support.local:8084": "http://support.local:8084/healthz",
	}
	for address, expected := range tests {
		actual, err := healthcheckURL(address)
		if err != nil {
			t.Fatalf("healthcheckURL(%q): %v", address, err)
		}
		if actual != expected {
			t.Fatalf("healthcheckURL(%q) = %q, want %q", address, actual, expected)
		}
	}
}

func TestCheckHealthProbesHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			t.Fatalf("unexpected health path %q", request.URL.Path)
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := checkHealth(strings.TrimPrefix(server.URL, "http://")); err != nil {
		t.Fatalf("checkHealth: %v", err)
	}
}

func TestCheckHealthRejectsUnhealthyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := checkHealth(strings.TrimPrefix(server.URL, "http://")); err == nil {
		t.Fatal("checkHealth unexpectedly accepted an unhealthy response")
	}
}
