package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cafedesk/apps/pos/api"
)

// syncClient calls the server's till endpoints: pair, changes, events.
type syncClient struct {
	base  string
	token string
	http  *http.Client
}

var errUnpaired = errors.New("this till isn't paired, or was unpaired in the admin")

func newSyncClient(base, token string) *syncClient {
	return &syncClient{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// checkServerURL allows https anywhere, and plain http only to this PC or
// the shop's own network, where a server in the shop would be.
func checkServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%q isn't a web address, such as https://example.up.railway.app", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
			return nil
		}
		return errors.New("use an https:// address: plain http is only for this PC or the shop's own network")
	}
	return fmt.Errorf("%q isn't a web address, such as https://example.up.railway.app", raw)
}

func (c *syncClient) do(ctx context.Context, method, path string, body, out any) error {
	var r *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	} else {
		r = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach the server: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && c.token != "" {
		return errUnpaired
	}
	if resp.StatusCode >= 300 {
		var e api.Error
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error.Message != "" {
			return errors.New(e.Error.Message)
		}
		return fmt.Errorf("the server answered %s", resp.Status)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("read the server's answer: %w", err)
		}
	}
	return nil
}

// pairWith pairs this till with a code from the admin.
func pairWith(ctx context.Context, base, code string) (api.Pairing, error) {
	var p api.Pairing
	err := newSyncClient(base, "").do(ctx, http.MethodPost, "/v1/terminal/pair", api.PairRequest{Code: code}, &p)
	return p, err
}

func (c *syncClient) changes(ctx context.Context, since int64) (api.Changes, error) {
	var ch api.Changes
	err := c.do(ctx, http.MethodGet, "/v1/terminal/changes?since="+strconv.FormatInt(since, 10), nil, &ch)
	return ch, err
}

func (c *syncClient) send(ctx context.Context, events []api.Event) (api.EventResult, error) {
	var res api.EventResult
	err := c.do(ctx, http.MethodPost, "/v1/terminal/events", api.EventBatch{Events: events}, &res)
	return res, err
}
