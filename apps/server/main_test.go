package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("GET /healthz = %d %q", rec.Code, rec.Body.String())
	}
}

func TestPing(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ping", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/ping = %d", rec.Code)
	}
	var p Ping
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	if p.Status != "ok" || p.ServerTime.IsZero() {
		t.Errorf("ping = %+v", p)
	}
}

func TestUnknownMethod(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/ping", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /v1/ping = %d, want 405", rec.Code)
	}
}
