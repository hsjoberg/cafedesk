package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cafedesk/apps/pos/api"
	"cafedesk/pkg/money"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
)

// memSecrets keeps secrets in memory, as the Credential Manager would.
type memSecrets map[string][]byte

func (m memSecrets) Get(k string) ([]byte, error) {
	v, ok := m[k]
	if !ok {
		return nil, mygo.ErrSecretNotFound
	}
	return v, nil
}
func (m memSecrets) Set(k string, v []byte) error { m[k] = v; return nil }
func (m memSecrets) Delete(k string) error        { delete(m, k); return nil }

// fakeServer answers the till endpoints as the server does, with one shop,
// a menu of one item, and two staff.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	events   []api.Event
	unpaired bool
	reject   api.EventType // turn events of this type away
	shop     uuid.UUID
	ravi     uuid.UUID
	raviHash string
	currency api.Currency // the business's
}

const fakeToken = "device-token"

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{shop: uuid.New(), ravi: uuid.New(), raviHash: hashPIN("5678"), currency: api.Currency{Code: "AED", Decimals: 2}}
	tea, karak, sugar, normal, less := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/terminal/pair", func(w http.ResponseWriter, r *http.Request) {
		var in api.PairRequest
		json.NewDecoder(r.Body).Decode(&in)
		if in.Code != "12345678" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "bad_code", "message": "That code doesn't work."}})
			return
		}
		json.NewEncoder(w).Encode(api.Pairing{Token: fakeToken,
			Organization: api.Organization{Id: uuid.New(), Name: "Test Org", Currency: f.currency.Code,
				CurrencyDecimals: f.currency.Decimals, Country: api.AE, VatPercent: 5},
			Shop:     api.Shop{Id: f.shop, Name: "Downtown", Timezone: "Asia/Dubai"},
			Terminal: api.Terminal{Id: uuid.New(), ShopId: f.shop, Code: "T2", Status: api.Paired}})
	})
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.unpaired || r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(401)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /v1/terminal/changes", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		ch := api.Changes{Version: 7, Full: r.URL.Query().Get("since") == "0", Categories: []api.SyncCategory{},
			Items: []api.SyncItem{}, ModifierGroups: []api.SyncModifierGroup{}, Staff: []api.SyncStaff{},
			Shop: api.ReceiptHeader{TradeName: "Karak Corner Cafeteria", TradeNameAr: "كافتيريا ركن الكرك",
				Address: "Downtown, Dubai", AddressAr: "وسط المدينة، دبي"}}
		f.mu.Lock()
		ch.Currency = f.currency
		f.mu.Unlock()
		if ch.Full {
			ch.Categories = []api.SyncCategory{{Id: tea, Name: "Hot drinks", Color: "#2F6690", Kitchen: false}}
			ch.ModifierGroups = []api.SyncModifierGroup{{Id: sugar, Name: "Sugar", Modifiers: []api.SyncModifier{
				{Id: normal, Name: "Normal sugar", IsDefault: true}, {Id: less, Name: "Less sugar", Position: 1}}}}
			ch.Items = []api.SyncItem{{Id: karak, CategoryId: tea, Name: "Karak", PriceFils: 120, ModifierGroupIds: []uuid.UUID{sugar}}}
			ch.Staff = []api.SyncStaff{
				{Id: f.ravi, Name: "Ravi", Role: api.StaffRoleManager, PinHash: f.raviHash, MustChangePin: true, Active: true},
				{Id: uuid.New(), Name: "Anil", Role: api.StaffRoleCashier, PinHash: hashPIN("2468"), Active: true},
			}
		}
		json.NewEncoder(w).Encode(ch)
	})
	mux.HandleFunc("POST /v1/terminal/events", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		var in api.EventBatch
		json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		defer f.mu.Unlock()
		res := api.EventResult{Accepted: []int64{}}
		for _, ev := range in.Events {
			if ev.Type == f.reject {
				res.Rejected = &api.EventRejection{Seq: ev.Seq, Message: "not today"}
				break
			}
			f.events = append(f.events, ev)
			res.Accepted = append(res.Accepted, ev.Seq)
		}
		json.NewEncoder(w).Encode(res)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) received(typ api.EventType) []api.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []api.Event
	for _, ev := range f.events {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// connect types codes on the setup screen: a wrong one, then the right one.
func connect(t *testing.T, a *app, tt *ui.Tester) {
	t.Helper()
	if !tt.HasText("Connect this till to your admin.") {
		t.Fatalf("no setup screen; texts %q", tt.Texts())
	}
	for _, d := range "11111111" {
		mustClick(t, tt, string(d))
	}
	if !tt.HasText("That code doesn't work.") {
		t.Fatalf("a wrong code; texts %q", tt.Texts())
	}
	for _, d := range "12345678" {
		mustClick(t, tt, string(d))
	}
	if a.needsSetup() {
		t.Fatalf("still setting up: %+v, sync %+v", a.pairing, a.sync)
	}
}

// connectedTill is a new till connected to the fake server, with Ravi
// signed in (with his own PIN, 9090) and a day open.
func connectedTill(t *testing.T) (*app, *fakeServer, memSecrets, *ui.Tester) {
	t.Helper()
	f := newFakeServer(t)
	a, _ := newTestAppMode(t, false)
	secrets := memSecrets{}
	a.secrets = secrets
	a.cfg.ServerURL = f.URL
	tt := ui.NewTester(a.view, 1366, 768)
	connect(t, a, tt)
	tt.Frame()
	typePIN(t, tt, "5678")
	typePIN(t, tt, "9090")
	typePIN(t, tt, "9090")
	if a.staff == nil || a.staff.Name != "Ravi" {
		t.Fatalf("signed in as %+v; texts %q", a.staff, tt.Texts())
	}
	a.float.set(money.FromMajor(100))
	a.startDay()
	tt.Frame()
	return a, f, secrets, tt
}

// A connected till that draws its first frame before reading its token
// doesn't then ask for a new code.
func TestStartConnected(t *testing.T) {
	a, _ := newTestAppMode(t, false)
	a.cfg.ShopID, a.synced = uuid.NewString(), true
	tt := ui.NewTester(a.view, 1366, 768)
	tt.Frame()
	a.secrets = memSecrets{tokenKey: []byte(fakeToken)}
	a.loadToken()
	tt.Frame()
	if a.pairing != nil || tt.HasText("Connect with a new code") {
		t.Fatalf("asks for a code; texts %q", tt.Texts())
	}
}

// A till counts in its business's currency: from the admin when it
// connects, and again if the admin changes it.
func TestCurrencyFromAdmin(t *testing.T) {
	t.Cleanup(func() { money.Use(money.AED) })
	f := newFakeServer(t)
	f.currency = api.Currency{Code: "KWD", Decimals: 3}
	a, _ := newTestAppMode(t, false)
	a.secrets = memSecrets{}
	a.cfg.ServerURL = f.URL
	tt := ui.NewTester(a.view, 1366, 768)
	connect(t, a, tt)
	if money.Current().Code != "KWD" || a.cfg.Currency.Decimals != 3 {
		t.Fatalf("currency %+v, config %+v", money.Current(), a.cfg.Currency)
	}
	if saved, err := loadConfig(a.cfgDir); err != nil || saved.Currency.Code != "KWD" {
		t.Errorf("not kept: %+v, %v", saved.Currency, err)
	}
	if got := a.catalog.Items[0].Price.WithCode(); got != "KWD 0.120" {
		t.Errorf("a price: %q", got)
	}
	f.mu.Lock()
	f.currency = api.Currency{Code: "USD", Decimals: 2}
	f.mu.Unlock()
	a.syncNow()
	if money.Current().Code != "USD" || a.cfg.Currency.Code != "USD" {
		t.Errorf("after a change: %+v", money.Current())
	}
	// A till started again counts in the one it kept.
	money.Use(money.AED)
	cfg, _ := loadConfig(a.cfgDir)
	newApp(cfg, a.cfgDir)
	if money.Current().Code != "USD" {
		t.Errorf("after a start: %+v", money.Current())
	}
}

func TestConnectAndSync(t *testing.T) {
	a, f, secrets, tt := connectedTill(t)
	ctx := context.Background()
	if a.token != fakeToken || string(secrets[tokenKey]) != fakeToken {
		t.Errorf("token %q, kept %q", a.token, secrets[tokenKey])
	}
	if a.cfg.ShopID != f.shop.String() || a.cfg.TerminalCode != "T2" || a.cfg.ShopLabel != "Downtown" {
		t.Errorf("config after connecting %+v", a.cfg)
	}
	// The receipt's header comes from the admin, and is kept.
	if a.cfg.Header.TradeName != "Karak Corner Cafeteria" || a.cfg.Header.AddressAr != "وسط المدينة، دبي" {
		t.Errorf("header %+v", a.cfg.Header)
	}
	if saved, err := loadConfig(a.cfgDir); err != nil || saved.Header != a.cfg.Header {
		t.Errorf("header not saved: %+v, %v", saved.Header, err)
	}
	// The menu and staff are the admin's: there was never a demo menu or a
	// till Owner.
	if len(a.catalog.Categories) != 1 || a.catalog.Categories[0].Name != "Hot drinks" || len(a.catalog.Items) != 1 ||
		a.catalog.Items[0].Price != 120 || len(a.catalog.GroupsOf(a.catalog.Items[0])) != 1 {
		t.Fatalf("catalog %+v", a.catalog)
	}
	if staff, _ := a.store.AllStaff(ctx); len(staff) != 2 {
		t.Errorf("staff %+v", staff)
	}

	// Ravi's own PIN went up, with the one it replaced.
	pins := f.received(api.EventTypeStaffPin)
	if len(pins) != 1 || pins[0].StaffPin.StaffId != f.ravi || pins[0].StaffPin.PreviousPinHash != f.raviHash ||
		!checkPIN(pins[0].StaffPin.PinHash, "9090") {
		t.Fatalf("PIN change sent: %+v", pins)
	}

	// A sale goes up with the till's name, after its day.
	mustClick(t, tt, "Karak")
	a.startPayment()
	a.method = payCard
	a.completeSale()
	orders := f.received(api.EventTypeOrder)
	if len(f.received(api.EventTypeBusinessDay)) != 1 || len(orders) != 1 || orders[0].Order.Code != "T2-001" ||
		orders[0].Order.StaffName != "Ravi" || orders[0].Order.Lines[0].Modifiers[0].Name != "Normal sugar" {
		t.Fatalf("events %+v", f.events)
	}
	if a.sync.pending != 0 || a.sync.err != "" {
		t.Errorf("sync state %+v", a.sync)
	}
	if msg, bad := a.syncSummary(); bad || !strings.HasPrefix(msg, "Up to date with the admin at") {
		t.Errorf("summary %q", msg)
	}
}

func TestOfflineThenSends(t *testing.T) {
	a, f, _, _ := connectedTill(t)
	a.cfg.ServerURL = "http://127.0.0.1:1" // nothing answers there
	for range 2 {
		a.addItem(a.catalog.Items[0])
		a.startPayment()
		a.method = payCard
		a.completeSale()
	}
	if a.cart.Empty() == false || a.sync.err == "" || a.sync.pending != 2 {
		t.Fatalf("offline: cart empty %v, sync %+v", a.cart.Empty(), a.sync)
	}
	tt := ui.NewTester(a.view, 1366, 768)
	if !tt.HasText("Offline") {
		t.Errorf("no warning by the clock; texts %q", tt.Texts())
	}
	a.cfg.ServerURL = f.URL // back online
	a.syncNow()
	if a.sync.err != "" || a.sync.pending != 0 || len(f.received(api.EventTypeOrder)) != 2 {
		t.Errorf("after coming back: %+v, %d orders", a.sync, len(f.received(api.EventTypeOrder)))
	}
}

func TestDisconnectedTillKeepsSelling(t *testing.T) {
	a, f, _, _ := connectedTill(t)
	f.mu.Lock()
	f.unpaired = true
	f.mu.Unlock()
	a.addItem(a.catalog.Items[0])
	a.startPayment()
	a.method = payCard
	a.completeSale()
	if !a.cart.Empty() {
		t.Fatal("the sale didn't complete")
	}
	if !a.sync.unpaired || a.sync.pending != 1 || a.paired() || a.needsSetup() {
		t.Errorf("sync state %+v", a.sync)
	}
	if msg, bad := a.syncSummary(); !bad || !strings.Contains(msg, "disconnected in the admin") {
		t.Errorf("summary %q", msg)
	}
	tt := ui.NewTester(a.view, 1366, 768)
	if !tt.HasText("Disconnected") {
		t.Errorf("no warning by the clock; texts %q", tt.Texts())
	}
}

func TestRejectedEventStopsTheQueue(t *testing.T) {
	a, f, _, _ := connectedTill(t)
	f.mu.Lock()
	f.reject = api.EventTypeOrder
	f.mu.Unlock()
	for range 2 {
		a.addItem(a.catalog.Items[0])
		a.startPayment()
		a.method = payCard
		a.completeSale()
	}
	if a.sync.rejected != "not today" || a.sync.pending != 2 {
		t.Errorf("sync state %+v", a.sync)
	}
	tt := ui.NewTester(a.view, 1366, 768)
	if !tt.HasText("Not sending") {
		t.Errorf("no warning by the clock; texts %q", tt.Texts())
	}
	// Once the admin takes it, the rest follows.
	f.mu.Lock()
	f.reject = ""
	f.mu.Unlock()
	a.syncNow()
	if a.sync.pending != 0 || a.sync.rejected != "" {
		t.Errorf("after the admin took them: %+v", a.sync)
	}
}

// A till that was tried out in demo mode keeps its test sales apart when
// it is connected: they never reach the admin.
func TestDemoSalesSetAside(t *testing.T) {
	f := newFakeServer(t)
	a, _ := newTestApp(t) // demo mode
	openTill(t, a)
	a.addItem(a.catalog.Items[0])
	a.startPayment()
	a.method = payCard
	a.completeSale()

	a.demo = false
	a.secrets = memSecrets{}
	a.cfg.ServerURL = f.URL
	a.staff = nil
	tt := ui.NewTester(a.view, 1366, 768)
	connect(t, a, tt)
	if len(f.received(api.EventTypeOrder)) != 0 || len(f.received(api.EventTypeBusinessDay)) != 0 {
		t.Errorf("demo sales reached the admin: %+v", f.events)
	}
	aside, _ := filepath.Glob(filepath.Join(a.cfgDir, "pos-demo-*.db"))
	if len(aside) != 1 {
		t.Errorf("demo database set aside as %v", aside)
	}
	if old, _ := a.store.HasHistory(context.Background()); old || a.day != nil {
		t.Errorf("the till still has the demo day or sales")
	}
	if !strings.Contains(a.settings.msg.text, "kept apart") {
		t.Errorf("message %q", a.settings.msg.text)
	}
}

func TestServerURLs(t *testing.T) {
	for url, ok := range map[string]bool{
		"https://cafedesk.up.railway.app": true,
		"http://localhost:8080":           true,
		"http://192.168.1.20:8080":        true,
		"http://example.com":              false,
		"ftp://example.com":               false,
		"cafedesk":                        false,
	} {
		if err := checkServerURL(url); (err == nil) != ok {
			t.Errorf("checkServerURL(%q) = %v", url, err)
		}
	}
}

func TestDemoTillDoesntSync(t *testing.T) {
	a, _ := newTestApp(t)
	openTill(t, a)
	a.syncNow() // no token: nothing happens, nothing breaks
	if a.sync.busy || a.sync.err != "" || a.needsSetup() {
		t.Errorf("sync state %+v", a.sync)
	}
	if _, err := memSecrets(nil).Get(tokenKey); !errors.Is(err, mygo.ErrSecretNotFound) {
		t.Error("memSecrets")
	}
}
