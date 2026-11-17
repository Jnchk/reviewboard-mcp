package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reviewboard-mcp/internal/config"
)

func TestWithToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := withToken("secret", next)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("with token: status %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d", rec.Code)
	}
}

func TestInstructionsReadOnly(t *testing.T) {
	cfg := &config.Config{URL: "https://rb.example.com", TextType: "markdown", ReadOnly: true}
	got := instructions(cfg, "alice")
	if got == "" || !strings.Contains(got, "read-only") || !strings.Contains(got, "https://rb.example.com") {
		t.Fatalf("instructions = %q", got)
	}
}
