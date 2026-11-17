package server_test

import (
	"strings"
	"testing"
	"time"

	"cafedesk/apps/server/internal/api"

	"github.com/google/uuid"
)

func TestShopDetails(t *testing.T) {
	c, _, _ := newServer(t)
	_, token, _, _ := tillSetup(t, c)
	till := newClient(t, c.ts)
	var me api.Me
	c.do("GET", "/v1/admin/me", nil, &me)
	shop := me.Shops[0]
	if shop.TradeName != "" || shop.Address != "" {
		t.Errorf("a new shop has details: %+v", shop)
	}

	in := api.ShopInput{Name: " Downtown ", TradeName: "Karak Corner Cafeteria", TradeNameAr: "كافتيريا ركن الكرك",
		Address: "Shop 3, Downtown, Dubai", AddressAr: "محل 3، وسط المدينة، دبي", Phone: "04 123 4567", LicenceNumber: "1234567"}
	var got api.Shop
	if code := c.do("PUT", "/v1/admin/shops/"+shop.Id.String(), in, &got); code != 200 ||
		got.Name != "Downtown" || got.TradeNameAr != in.TradeNameAr || got.LicenceNumber != "1234567" {
		t.Fatalf("update: %d %+v", code, got)
	}
	c.do("GET", "/v1/admin/me", nil, &me)
	if me.Shops[0].Address != in.Address {
		t.Errorf("me: %+v", me.Shops[0])
	}

	// The tills get it with every sync, changes or not.
	var ch api.Changes
	till.do("GET", "/v1/terminal/changes?since=0", nil, &ch, bearer(token)...)
	if ch.Shop.TradeName != in.TradeName || ch.Shop.AddressAr != in.AddressAr || ch.Shop.Phone != in.Phone {
		t.Errorf("full changes: %+v", ch.Shop)
	}
	till.do("GET", "/v1/terminal/changes?since="+itoa(ch.Version), nil, &ch, bearer(token)...)
	if ch.Shop.TradeName != in.TradeName {
		t.Errorf("no changes: %+v", ch.Shop)
	}

	var e api.Error
	if code := c.do("PUT", "/v1/admin/shops/"+shop.Id.String(), api.ShopInput{Name: " "}, &e); code != 400 {
		t.Errorf("no name: %d", code)
	}
	long := in
	long.Phone = strings.Repeat("1", 41)
	if code := c.do("PUT", "/v1/admin/shops/"+shop.Id.String(), long, &e); code != 400 || e.Error.Field == nil ||
		*e.Error.Field != "phone" {
		t.Errorf("a long phone: %d %+v", code, e)
	}
	if code := c.do("PUT", "/v1/admin/shops/"+uuid.NewString(), in, &e); code != 404 {
		t.Errorf("no such shop: %d", code)
	}
}

func TestVatStatus(t *testing.T) {
	c, _, _ := newServer(t)
	_, token, manager, _ := tillSetup(t, c)
	till := newClient(t, c.ts)
	var st api.VatStatus
	if code := c.do("GET", "/v1/admin/reports/vat", nil, &st); code != 200 || st.SalesFils != 0 ||
		st.MandatoryFils != 37_500_000 || st.VoluntaryFils != 18_750_000 || st.Registered {
		t.Fatalf("empty: %d %+v", code, st)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	send := func(seq int64, ev api.Event) {
		var res api.EventResult
		ev.Seq = seq
		till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{ev}}, &res, bearer(token)...)
		if len(res.Accepted) != 1 {
			t.Fatalf("event %d: %+v", seq, res)
		}
	}
	day := func(seq int64, at time.Time) uuid.UUID {
		id := uuid.New()
		send(seq, api.Event{Type: api.EventTypeBusinessDay, BusinessDay: &api.BusinessDayEvent{Id: id,
			Date: at.Format("2006-01-02"), OpenedAt: at, OpenedBy: manager.Id, OpenedByName: "Ravi"}})
		return id
	}
	sale := func(seq int64, dayID uuid.UUID, at time.Time, fils int64) {
		o := api.OrderEvent{Id: uuid.New(), BusinessDayId: dayID, Number: int(seq), Code: "T1-" + itoa(seq), CreatedAt: at,
			StaffId: manager.Id, StaffName: "Ravi", TotalFils: fils, PaymentMethod: api.Cash, TenderedFils: fils,
			Lines: []api.OrderLineEvent{{Id: uuid.New(), ItemId: "karak", Name: "Karak chai", Qty: 1, UnitPriceFils: fils,
				LineTotalFils: fils, Modifiers: []api.OrderLineModifierEvent{}}}}
		send(seq, api.Event{Type: api.EventTypeOrder, Order: &o})
	}
	recent := day(1, now.Add(-48*time.Hour))
	sale(2, recent, now.Add(-47*time.Hour), 300_000_00)
	old := day(3, now.AddDate(-1, 0, -10)) // more than 12 months ago: not counted
	sale(4, old, now.AddDate(-1, 0, -10), 100_000_00)
	c.do("GET", "/v1/admin/reports/vat", nil, &st)
	if st.SalesFils != 300_000_00 {
		t.Errorf("12 months: %+v", st)
	}
}
