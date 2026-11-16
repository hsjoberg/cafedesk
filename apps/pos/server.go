package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// pingResult is what GET /v1/ping told the terminal.
type pingResult struct {
	Version    string
	ServerTime time.Time
	Latency    time.Duration
}

// pingServer calls the server's /v1/ping, to prove the terminal can reach
// it and to compare clocks.
func pingServer(ctx context.Context, baseURL string) (pingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	url := strings.TrimRight(baseURL, "/") + "/v1/ping"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return pingResult{}, err
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return pingResult{}, err
	}
	defer resp.Body.Close()
	latency := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		return pingResult{}, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	var body struct {
		Status     string    `json:"status"`
		Version    string    `json:"version"`
		ServerTime time.Time `json:"serverTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return pingResult{}, fmt.Errorf("read answer: %w", err)
	}
	if body.Status != "ok" {
		return pingResult{}, fmt.Errorf("server status %q", body.Status)
	}
	return pingResult{Version: body.Version, ServerTime: body.ServerTime, Latency: latency}, nil
}
