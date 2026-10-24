package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDegradedHandlerKeepsAgentReachable(t *testing.T) {
	status := httptest.NewRecorder()
	degradedHandler{}.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", status.Code, http.StatusOK)
	}
	if !strings.Contains(status.Body.String(), `"storage_status":"unavailable"`) {
		t.Fatalf("status body = %s", status.Body.String())
	}

	records := httptest.NewRecorder()
	degradedHandler{}.ServeHTTP(records, httptest.NewRequest(http.MethodGet, "/api/records", nil))
	if records.Code != http.StatusServiceUnavailable {
		t.Fatalf("records code = %d, want %d", records.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(records.Body.String(), `"code":"storage_unavailable"`) {
		t.Fatalf("records body = %s", records.Body.String())
	}
}
