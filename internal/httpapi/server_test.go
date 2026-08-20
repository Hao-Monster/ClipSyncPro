package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clipsyncpro/internal/storage"
)

func TestServerDoesNotGrantCrossOriginBrowserAccess(t *testing.T) {
	server := NewServer(storage.NewMemoryStore(), "")
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/clipboard/records", nil)
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected cross-origin access: %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header: %q", rr.Header().Get("X-Content-Type-Options"))
	}
}
