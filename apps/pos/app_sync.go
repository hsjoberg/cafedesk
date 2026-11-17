package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cafedesk/pkg/money"

	"github.com/egoist/mygo"
)

// Pairing and syncing, from the app's side: the till works on its own
// either way, and syncs whenever it is paired and online.

const tokenKey = "terminal-token"

// secretStore keeps the device token: Windows's Credential Manager in the
// app (mygo.Secrets), a map in tests.
type secretStore interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

// syncState is how syncing went, for the screen.
type syncState struct {
	busy     bool
	again    bool // asked for while busy: sync again after
	lastOK   time.Time
	err      string // the last attempt's problem, empty if it worked
	rejected string // an event the server turned away
	pending  int
	unpaired bool // the server says this till isn't paired
}

// pairState is the pairing code being typed.
type pairState struct {
	code string
	err  string
	busy bool
}

const pairCodeLength = 8

// paired reports whether the till has a device token for a real shop.
func (a *app) paired() bool { return a.token != "" && a.cfg.ShopID != "local" && !a.sync.unpaired }

// loadToken reads the device token, as the app starts.
func (a *app) loadToken() {
	if a.secrets == nil {
		return
	}
	b, err := a.secrets.Get(tokenKey)
	if err != nil {
		if !errors.Is(err, mygo.ErrSecretNotFound) {
			a.sync.err = "Couldn't read the till's pairing: " + err.Error()
		}
		return
	}
	a.token = string(b)
	// The window may have drawn the setup screen before the token was read,
	// and it opens a code pad for a till that isn't connected: this one is.
	a.pairing = nil
}

func (a *app) openPairing() {
	if err := checkServerURL(a.cfg.ServerURL); err != nil {
		a.settings.msg = errResult("Set the admin's address first: ", err)
		return
	}
	a.pairing = &pairState{}
}

func (a *app) pairKey(k string) {
	p := a.pairing
	if p == nil || p.busy {
		return
	}
	p.err = ""
	switch k {
	case "back":
		if p.code != "" {
			p.code = p.code[:len(p.code)-1]
		}
	case "clear":
		p.code = ""
	default:
		if len(p.code) < pairCodeLength {
			p.code += k
		}
	}
	if len(p.code) == pairCodeLength {
		if err := checkServerURL(a.cfg.ServerURL); err != nil {
			p.code, p.err = "", "Set the admin's address first: "+err.Error()
			return
		}
		a.pair(p.code)
	}
}

// pair connects the till with a code from the admin: it keeps the device
// token, takes its shop and name from the server, and fetches the menu and
// staff afresh. The first time, a till that holds sales from trying it out
// sets them aside, out of the admin's reports.
func (a *app) pair(code string) {
	p, store, secrets, base := a.pairing, a.store, a.secrets, a.cfg.ServerURL
	first := a.cfg.ShopID == "local"
	p.busy = true
	a.async(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pairing, err := pairWith(ctx, base, code)
		if err == nil && secrets != nil {
			err = secrets.Set(tokenKey, []byte(pairing.Token))
		}
		aside := ""
		if err == nil && store != nil && first {
			if old, herr := store.HasHistory(ctx); herr != nil {
				err = herr
			} else if old {
				aside, err = store.StartFresh(ctx, "demo")
			}
		}
		if err == nil && store != nil {
			err = store.ResetSync(ctx)
		}
		return func() {
			p.busy, p.code = false, ""
			if err != nil {
				p.err = err.Error()
				return
			}
			a.pairing = nil
			a.token = pairing.Token
			a.cfg.OrgID, a.cfg.ShopID, a.cfg.TerminalID = pairing.Organization.Id.String(), pairing.Shop.Id.String(),
				pairing.Terminal.Id.String()
			a.cfg.TerminalCode = pairing.Terminal.Code
			a.cfg.ShopLabel = pairing.Shop.Name
			a.saveConfig()
			a.useCurrency(money.Currency{Code: pairing.Organization.Currency, Decimals: pairing.Organization.CurrencyDecimals})
			a.sync = syncState{}
			if first {
				a.synced = false
				a.day, a.lastOrder, a.lastCode, a.nextNumber = nil, nil, "", 1
				a.firstDay, a.carry = true, 0
				a.setCatalog(&Catalog{Groups: map[string]ModifierGroup{}})
			}
			msg := fmt.Sprintf("Connected as %s of %s.", pairing.Terminal.Code, pairing.Shop.Name)
			if aside != "" {
				msg += " The sales from trying the till out are kept apart, in " + aside + "."
			}
			a.settings.msg = result{text: msg}
			a.syncNow()
		}
	})
}

// syncNow pulls changes and sends events, in the background.
func (a *app) syncNow() {
	if !a.paired() || a.store == nil {
		return
	}
	if a.sync.busy {
		a.sync.again = true
		return
	}
	a.sync.busy = true
	client, store := newSyncClient(a.cfg.ServerURL, a.token), a.store
	a.async(func() func() {
		res := runSync(context.Background(), client, store)
		return func() {
			a.sync.busy = false
			a.syncDone(res)
			if a.sync.again {
				a.sync.again = false
				a.syncNow()
			}
		}
	})
}

type syncResult struct {
	catalog  *Catalog       // the menu, when it changed
	header   *ReceiptHeader // the shop's details for receipts, as the admin has them
	currency money.Currency // the business's, as the admin has it; empty if not sent
	applied  bool           // the server's changes were saved
	err      error
	rejected string
	pending  int
}

// runSync pulls what changed since the last sync, then sends what waits,
// oldest first, until all is sent or the server turns an event away.
func runSync(ctx context.Context, c *syncClient, st *Store) (res syncResult) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Counted last, whatever happened: the result is named so this sets it.
	defer func() { res.pending, _ = st.PendingCount(context.Background()) }()

	since, err := st.SyncVersion(ctx)
	if err != nil {
		res.err = err
		return res
	}
	ch, err := c.changes(ctx, since)
	if err != nil {
		res.err = err
		return res
	}
	res.header = &ReceiptHeader{TradeName: ch.Shop.TradeName, TradeNameAr: ch.Shop.TradeNameAr, Address: ch.Shop.Address,
		AddressAr: ch.Shop.AddressAr, Phone: ch.Shop.Phone, LicenceNumber: ch.Shop.LicenceNumber}
	res.currency = money.Currency{Code: ch.Currency.Code, Decimals: ch.Currency.Decimals}
	if ch.Version != since || ch.Full {
		if err := st.ApplyChanges(ctx, ch); err != nil {
			res.err = fmt.Errorf("save the menu from the admin: %w", err)
			return res
		}
		if len(ch.Categories)+len(ch.Items)+len(ch.ModifierGroups) > 0 || ch.Full {
			if c, err := st.LoadCatalog(ctx, false); err == nil {
				res.catalog = c
			}
		}
	}
	if since > 0 || ch.Version > 0 {
		res.applied = true
	}
	for range 50 {
		events, err := st.PendingEvents(ctx, 100)
		if err != nil {
			res.err = err
			return res
		}
		if len(events) == 0 {
			break
		}
		sent, err := c.send(ctx, events)
		if err != nil {
			res.err = err
			return res
		}
		if err := st.MarkSent(ctx, sent.Accepted, time.Now()); err != nil {
			res.err = err
			return res
		}
		if sent.Rejected != nil {
			res.rejected = sent.Rejected.Message
			break
		}
		if len(sent.Accepted) < len(events) {
			break
		}
	}
	return res
}

func (a *app) syncDone(res syncResult) {
	switch {
	case errors.Is(res.err, errUnpaired):
		a.sync.unpaired = true
		a.sync.err = "This till was disconnected in the admin. It keeps selling; connect it again in Settings, with a new code, to send its sales."
	case res.err != nil:
		a.sync.err = res.err.Error()
	default:
		a.sync.err = ""
		a.sync.lastOK = time.Now()
	}
	a.sync.rejected = res.rejected
	a.sync.pending = res.pending
	if res.applied {
		a.synced = true
	}
	if res.catalog != nil {
		a.setCatalog(res.catalog)
	}
	if res.header != nil && *res.header != a.cfg.Header {
		a.cfg.Header = *res.header
		a.saveConfig()
	}
	a.useCurrency(res.currency)
}

// useCurrency counts in the business's currency, from the admin, and keeps
// it for the next start. One the till doesn't know is left alone.
func (a *app) useCurrency(c money.Currency) {
	if _, ok := money.LookupCurrency(c.Code); !ok || c == a.cfg.Currency {
		return
	}
	a.cfg.Currency = c
	money.Use(a.cfg.currency())
	a.saveConfig()
}

// syncSummary says, in a sentence, how the till stands with the admin.
func (a *app) syncSummary() (string, bool) {
	s := a.sync
	switch {
	case a.token == "" || a.cfg.ShopID == "local":
		return "Not connected to an admin.", false
	case s.unpaired:
		return s.err, true
	case s.err != "":
		return "Can't reach the admin, so sales wait on this till: " + s.err, true
	case s.rejected != "":
		return "The admin turned away a change, so sending has stopped: " + s.rejected, true
	case s.lastOK.IsZero():
		return "Not updated yet.", false
	case s.pending == 1:
		return "Up to date at " + s.lastOK.Format("15:04") + ". 1 change waiting to send.", false
	case s.pending > 1:
		return fmt.Sprintf("Up to date at %s. %d changes waiting to send.", s.lastOK.Format("15:04"), s.pending), false
	}
	return "Up to date with the admin at " + s.lastOK.Format("15:04") + ".", false
}
