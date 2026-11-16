package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ping" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"test","serverTime":"2026-11-16T12:00:00Z"}`))
	}))
	defer srv.Close()

	p, err := pingServer(context.Background(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "test" || p.ServerTime.IsZero() {
		t.Errorf("ping = %+v", p)
	}

	if _, err := pingServer(context.Background(), srv.URL+"/nowhere"); err == nil {
		t.Error("a 404 counted as reaching the server")
	}
}
