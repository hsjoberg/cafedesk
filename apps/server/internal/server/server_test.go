package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/auth"
	"cafedesk/apps/server/internal/server"
	"cafedesk/apps/server/internal/store"
	"cafedesk/apps/server/internal/testdb"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ownerEmail    = "owner@example.com"
	ownerPassword = "correct horse battery"
)

type client struct {
	t    *testing.T
	ts   *httptest.Server
	http *http.Client
	pool *pgxpool.Pool
}

// newServer runs the server over TLS on a fresh database with one
// organization, and returns a client with no session yet.
func newServer(t *testing.T) (*client, *store.Store, uuid.UUID) {
	t.Helper()
	pool := testdb.New(t)
	st := store.New(pool)
	hash, _ := auth.HashPassword(ownerPassword)
	org, _, _, err := st.Setup(context.Background(), "Cafedesk", "Downtown", "Owner", ownerEmail, hash)
	if err != nil {
		t.Fatal(err)
	}
	admin := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Cafedesk</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	ts := httptest.NewTLSServer(server.New(st, server.Config{Version: "test", SecureCookies: true, Admin: admin}))
	t.Cleanup(ts.Close)
	c := newClient(t, ts)
	c.pool = pool
	return c, st, org.ID
}

// newClient returns a client with cookies of its own. ts.Client returns
// one shared client, so it is copied.
func newClient(t *testing.T, ts *httptest.Server) *client {
	hc := *ts.Client()
	hc.Jar, _ = cookiejar.New(nil)
	return &client{t: t, ts: ts, http: &hc}
}

// do sends a request with a JSON body (unless body is nil) and decodes a
// JSON answer into out (unless out is nil).
func (c *client) do(method, path string, body, out any, header ...string) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.ts.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (c *client) signIn() {
	c.t.Helper()
	if code := c.do("POST", "/v1/admin/session", api.SignIn{Email: ownerEmail, Password: ownerPassword}, nil); code != 200 {
		c.t.Fatalf("sign in: %d", code)
	}
}

func TestHealthAndPing(t *testing.T) {
	c, _, _ := newServer(t)
	var p api.Ping
	if code := c.do("GET", "/v1/ping", nil, &p); code != 200 || p.Status != api.Ok || p.Version != "test" {
		t.Errorf("ping %d %+v", code, p)
	}
	if code := c.do("GET", "/healthz", nil, nil); code != 200 {
		t.Errorf("healthz %d", code)
	}
}

func TestSignInAndOut(t *testing.T) {
	c, _, _ := newServer(t)
	var e api.Error
	if code := c.do("POST", "/v1/admin/session", api.SignIn{Email: ownerEmail, Password: "wrong password"}, &e); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	if code := c.do("POST", "/v1/admin/session", api.SignIn{Email: "nobody@example.com", Password: ownerPassword}, &e); code != 401 ||
		e.Error.Message != "That email and password don't match." {
		t.Fatalf("unknown email: %d %+v", code, e)
	}
	if code := c.do("GET", "/v1/admin/me", nil, nil); code != 401 {
		t.Fatalf("me without a session: %d", code)
	}

	req, _ := http.NewRequest("POST", c.ts.URL+"/v1/admin/session",
		strings.NewReader(`{"email":"OWNER@example.com","password":"`+ownerPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("sign in: %v %v", err, resp.Status)
	}
	var me api.Me
	json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	if me.User.Role != api.UserRole("owner") || me.Organization.Name != "Cafedesk" || len(me.Shops) != 1 {
		t.Errorf("me %+v", me)
	}
	cookie := resp.Cookies()[0]
	if cookie.Name != "cafedesk_session" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie %+v", cookie)
	}
	if code := c.do("GET", "/v1/admin/me", nil, &me); code != 200 {
		t.Fatalf("me: %d", code)
	}
	if code := c.do("DELETE", "/v1/admin/session", nil, nil); code != 204 {
		t.Fatalf("sign out: %d", code)
	}
	if code := c.do("GET", "/v1/admin/me", nil, nil); code != 401 {
		t.Errorf("me after signing out: %d", code)
	}
	// The old token no longer works, even if a copy of it is sent.
	c.http.Jar, _ = cookiejar.New(nil)
	req, _ = http.NewRequest("GET", c.ts.URL+"/v1/admin/me", nil)
	req.AddCookie(&http.Cookie{Name: "cafedesk_session", Value: cookie.Value})
	if resp, _ := c.http.Do(req); resp.StatusCode != 401 {
		t.Errorf("a signed-out token: %d", resp.StatusCode)
	}
}

// Over plain HTTP to an address of the shop's network, as a till's PC
// reaching a server in the shop, the cookie can't be HTTPS-only, or the
// browser drops it. To a public address it always is.
func TestCookieOverLocalHTTP(t *testing.T) {
	_, st, _ := newServer(t)
	plain := httptest.NewServer(server.New(st, server.Config{SecureCookies: true}))
	defer plain.Close()
	for host, secure := range map[string]bool{
		"192.168.1.46:8080":       false,
		"localhost:8080":          false,
		"cafedesk.up.railway.app": true,
		"example.com:8080":        true,
	} {
		req, _ := http.NewRequest("POST", plain.URL+"/v1/admin/session",
			strings.NewReader(`{"email":"`+ownerEmail+`","password":"`+ownerPassword+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || len(resp.Cookies()) == 0 {
			t.Fatalf("%s: %d", host, resp.StatusCode)
		}
		if got := resp.Cookies()[0].Secure; got != secure {
			t.Errorf("%s: Secure %v, want %v", host, got, secure)
		}
	}
}

func TestSignInRateLimit(t *testing.T) {
	c, _, _ := newServer(t)
	for i := 0; i < 10; i++ {
		c.do("POST", "/v1/admin/session", api.SignIn{Email: ownerEmail, Password: "wrong password"}, nil)
	}
	var e api.Error
	if code := c.do("POST", "/v1/admin/session", api.SignIn{Email: ownerEmail, Password: ownerPassword}, &e); code != 429 {
		t.Errorf("11th attempt: %d %+v", code, e)
	}
}

func TestGuards(t *testing.T) {
	c, _, _ := newServer(t)
	if code := c.do("GET", "/v1/admin/catalog", nil, nil); code != 401 {
		t.Errorf("catalog without a session: %d", code)
	}
	c.signIn()
	cat := api.CategoryInput{Name: "Tea", Color: "#5F8A3A"}
	if code := c.do("POST", "/v1/admin/categories", cat, nil, "Origin", "https://evil.example"); code != 403 {
		t.Errorf("another origin: %d", code)
	}
	if code := c.do("POST", "/v1/admin/categories", cat, nil, "Sec-Fetch-Site", "cross-site"); code != 403 {
		t.Errorf("cross-site fetch: %d", code)
	}
	if code := c.do("POST", "/v1/admin/categories", cat, nil, "Origin", c.ts.URL); code != 201 {
		t.Errorf("own origin: %d", code)
	}
	req, _ := http.NewRequest("POST", c.ts.URL+"/v1/admin/categories", strings.NewReader("name=Tea"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, _ := c.http.Do(req); resp.StatusCode != 415 {
		t.Errorf("a form post: %d", resp.StatusCode)
	}
}

func TestMenu(t *testing.T) {
	c, _, _ := newServer(t)
	c.signIn()
	var e api.Error

	// Validation.
	if code := c.do("POST", "/v1/admin/categories", api.CategoryInput{Name: " ", Color: "#5F8A3A"}, &e); code != 400 ||
		e.Error.Field == nil || *e.Error.Field != "name" {
		t.Errorf("empty name: %d %+v", code, e)
	}
	if code := c.do("POST", "/v1/admin/categories", api.CategoryInput{Name: "Tea", Color: "green"}, &e); code != 400 {
		t.Errorf("bad color: %d", code)
	}
	if code := c.do("POST", "/v1/admin/modifier-groups", api.ModifierGroupInput{Name: "Sugar",
		Modifiers: []api.ModifierInput{{Name: "A", IsDefault: true}, {Name: "B", IsDefault: true}}}, &e); code != 400 {
		t.Errorf("two defaults in a one-choice group: %d", code)
	}

	var sugar api.ModifierGroup
	if code := c.do("POST", "/v1/admin/modifier-groups", api.ModifierGroupInput{Name: "Sugar",
		Modifiers: []api.ModifierInput{{Name: "Normal sugar", IsDefault: true}, {Name: "Less sugar"}}}, &sugar); code != 201 {
		t.Fatalf("group: %d", code)
	}
	var tea api.Category
	if code := c.do("POST", "/v1/admin/categories", api.CategoryInput{Name: " Tea ", Color: "#5f8a3a"}, &tea); code != 201 ||
		tea.Name != "Tea" || tea.Color != "#5F8A3A" {
		t.Fatalf("category: %d %+v", code, tea)
	}
	var karak api.Item
	in := api.ItemInput{CategoryId: tea.Id, Name: "Karak chai", PriceFils: 100, ModifierGroupIds: []uuid.UUID{sugar.Id}}
	if code := c.do("POST", "/v1/admin/items", in, &karak); code != 201 || karak.Position != 0 {
		t.Fatalf("item: %d %+v", code, karak)
	}
	in.PriceFils = -1
	if code := c.do("PUT", "/v1/admin/items/"+karak.Id.String(), in, &e); code != 400 {
		t.Errorf("a negative price: %d", code)
	}
	in.PriceFils = 150
	if code := c.do("PUT", "/v1/admin/items/"+karak.Id.String(), in, &karak); code != 200 || karak.PriceFils != 150 {
		t.Errorf("update: %d %+v", code, karak)
	}

	var cat api.Catalog
	c.do("GET", "/v1/admin/catalog", nil, &cat)
	if len(cat.Items) != 1 || cat.Items[0].PriceFils != 150 || len(cat.Items[0].ModifierGroupIds) != 1 || cat.Version < 4 {
		t.Errorf("catalog %+v", cat)
	}

	if code := c.do("DELETE", "/v1/admin/categories/"+tea.Id.String(), nil, &e); code != 409 ||
		e.Error.Message != "This category still has 1 items. Move or delete them first." {
		t.Errorf("deleting a category with items: %d %+v", code, e)
	}
	if code := c.do("POST", "/v1/admin/items/"+uuid.NewString()+"/move", api.Move{Position: 0}, nil); code != 404 {
		t.Errorf("moving a missing item: %d", code)
	}
	if code := c.do("PUT", "/v1/admin/items/not-a-uuid", in, nil); code != 400 {
		t.Errorf("a bad ID: %d", code)
	}
	if code := c.do("POST", "/v1/admin/items", map[string]any{"name": "x", "surprise": 1}, nil); code != 400 {
		t.Errorf("an unknown field: %d", code)
	}
}

func TestOrganizationsDontMix(t *testing.T) {
	c, _, _ := newServer(t)
	c.signIn()
	var tea api.Category
	c.do("POST", "/v1/admin/categories", api.CategoryInput{Name: "Tea", Color: "#5F8A3A"}, &tea)

	// A second organization, made directly: setup is for the first.
	if err := otherOrg(c.pool); err != nil {
		t.Fatal(err)
	}
	c2 := newClient(t, c.ts)
	if code := c2.do("POST", "/v1/admin/session", api.SignIn{Email: "other@example.com", Password: ownerPassword}, nil); code != 200 {
		t.Fatalf("sign in to the other organization: %d", code)
	}
	var cat api.Catalog
	c2.do("GET", "/v1/admin/catalog", nil, &cat)
	if len(cat.Categories) != 0 {
		t.Errorf("another organization's menu shows: %+v", cat.Categories)
	}
	if code := c2.do("PUT", "/v1/admin/categories/"+tea.Id.String(), api.CategoryInput{Name: "Mine", Color: "#000000"}, nil); code != 404 {
		t.Errorf("changed another organization's category: %d", code)
	}
	if code := c2.do("DELETE", "/v1/admin/categories/"+tea.Id.String(), nil, nil); code != 404 {
		t.Errorf("deleted another organization's category: %d", code)
	}
}

func TestAdminFiles(t *testing.T) {
	c, _, _ := newServer(t)
	for path, want := range map[string]int{"/": 200, "/menu": 200, "/menu/items": 200, "/assets/app.js": 200, "/missing.png": 404} {
		resp, err := c.http.Get(c.ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s: %d, want %d", path, resp.StatusCode, want)
		}
		if path == "/assets/app.js" && !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("assets aren't cached: %q", resp.Header.Get("Cache-Control"))
		}
		if resp.Header.Get("Content-Security-Policy") == "" || resp.Header.Get("X-Frame-Options") != "DENY" {
			t.Errorf("GET %s lacks security headers", path)
		}
	}
}
