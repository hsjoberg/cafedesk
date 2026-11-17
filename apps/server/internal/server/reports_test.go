package server_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/store"

	"github.com/google/uuid"
)

// Two business days on one till: a closed one with cash, card, a void and a
// cash refund, and an open one with a sale after midnight.
func TestSalesReport(t *testing.T) {
	c, _, _ := newServer(t)
	shop, token, manager, cashier := tillSetup(t, c)
	till := newClient(t, c.ts)
	dubai, _ := time.LoadLocation("Asia/Dubai")
	at := func(date string, hour, min int) time.Time {
		d, _ := time.ParseInLocation("2006-01-02", date, dubai)
		return d.Add(time.Duration(hour)*time.Hour + time.Duration(min)*time.Minute).UTC()
	}
	line := func(item, name string, qty int, price int64) api.OrderLineEvent {
		return api.OrderLineEvent{Id: uuid.New(), ItemId: item, Name: name, Qty: qty, UnitPriceFils: price,
			LineTotalFils: int64(qty) * price, Modifiers: []api.OrderLineModifierEvent{}}
	}
	order := func(day uuid.UUID, n int, when time.Time, who api.Staff, pay api.PaymentMethod, lines ...api.OrderLineEvent) api.OrderEvent {
		var total int64
		for _, l := range lines {
			total += l.LineTotalFils
		}
		return api.OrderEvent{Id: uuid.New(), BusinessDayId: day, Number: n, Code: "T1-00" + itoa(int64(n)), CreatedAt: when,
			StaffId: who.Id, StaffName: who.Name, TotalFils: total, PaymentMethod: pay, TenderedFils: total, Lines: lines}
	}

	day1, day2 := uuid.New(), uuid.New()
	open1 := api.BusinessDayEvent{Id: day1, Date: "2026-11-14", OpenedAt: at("2026-11-14", 7, 0), OpenedBy: manager.Id,
		OpenedByName: "Ravi", OpeningFloatFils: 10000}
	o1 := order(day1, 1, at("2026-11-14", 8, 15), cashier, api.Cash, line("karak", "Karak chai", 2, 100), line("parotta", "Parotta", 1, 100))
	o2 := order(day1, 2, at("2026-11-14", 21, 30), manager, api.Card, line("karak", "Karak", 1, 150))
	o3 := order(day1, 3, at("2026-11-14", 21, 45), cashier, api.Cash, line("karak", "Karak chai", 1, 100))
	closed1 := open1
	// Expected 10000 + 400 cash - 100 refunded = 10300; 10250 counted is 50 short.
	closed1.ClosedAt, closed1.ClosedBy, closed1.ClosedByName, closed1.CountedCashFils =
		ptr(at("2026-11-14", 23, 0)), &manager.Id, ptr("Ravi"), ptr(int64(10250))
	open2 := api.BusinessDayEvent{Id: day2, Date: "2026-11-15", OpenedAt: at("2026-11-15", 7, 0), OpenedBy: manager.Id,
		OpenedByName: "Ravi", OpeningFloatFils: 5000}
	o4 := order(day2, 1, at("2026-11-16", 0, 30), cashier, api.Card, line("parotta", "Parotta", 2, 100))
	voided := order(day2, 2, at("2026-11-15", 9, 0), cashier, api.Cash, line("karak", "Karak chai", 1, 100))
	// Voided as a test order: not counted at all, not even as a void.
	test := order(day2, 3, at("2026-11-15", 9, 10), cashier, api.Cash, line("karak", "Karak chai", 2, 100))

	var res api.EventResult
	till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{
		{Seq: 1, Type: api.EventTypeBusinessDay, BusinessDay: &open1},
		{Seq: 2, Type: api.EventTypeOrder, Order: &o1},
		{Seq: 3, Type: api.EventTypeOrder, Order: &o2},
		{Seq: 4, Type: api.EventTypeOrder, Order: &o3},
		{Seq: 5, Type: api.EventTypeRefund, Refund: &api.RefundEvent{Id: uuid.New(), OrderId: o1.Id, BusinessDayId: day1,
			CreatedAt: at("2026-11-14", 9, 0), StaffId: manager.Id, StaffName: "Ravi", ApprovedBy: manager.Id,
			ApprovedByName: "Ravi", Reason: "Wrong item", Method: api.Cash, TotalFils: 100,
			Lines: []api.RefundLineEvent{{OrderLineId: o1.Lines[1].Id, Qty: 1, AmountFils: 100}}}},
		{Seq: 6, Type: api.EventTypeBusinessDay, BusinessDay: &closed1},
		{Seq: 7, Type: api.EventTypeBusinessDay, BusinessDay: &open2},
		{Seq: 8, Type: api.EventTypeOrder, Order: &o4},
		{Seq: 9, Type: api.EventTypeOrder, Order: &voided},
		{Seq: 10, Type: api.EventTypeOrderVoid, OrderVoid: &api.OrderVoidEvent{OrderId: voided.Id, VoidedAt: at("2026-11-15", 9, 5),
			VoidedBy: manager.Id, VoidedByName: "Ravi", Reason: "Wrong item rung up"}},
		{Seq: 11, Type: api.EventTypeOrder, Order: &test},
		{Seq: 12, Type: api.EventTypeOrderVoid, OrderVoid: &api.OrderVoidEvent{OrderId: test.Id, VoidedAt: at("2026-11-15", 9, 12),
			VoidedBy: manager.Id, VoidedByName: "Ravi", Reason: store.TestOrderReason}},
	}}, &res, bearer(token)...)
	if len(res.Accepted) != 12 {
		t.Fatalf("events: %+v", res)
	}

	var rep api.SalesReport
	if code := c.do("GET", "/v1/admin/reports/sales?from=2026-11-14&to=2026-11-15", nil, &rep); code != 200 {
		t.Fatalf("report: %d", code)
	}
	want := api.SalesTotals{Orders: 4, GrossFils: 750, CashFils: 400, CardFils: 350, Refunds: 1, RefundsFils: 100,
		RefundsCashFils: 100, Voids: 1, VoidsFils: 100, NetFils: 650, UncostedFils: 750}
	if rep.Totals != want {
		t.Errorf("totals %+v, want %+v", rep.Totals, want)
	}
	if len(rep.Days) != 2 || rep.Days[0] != (api.DaySales{Date: "2026-11-14", Orders: 3, GrossFils: 550, RefundsFils: 100, NetFils: 450}) ||
		rep.Days[1] != (api.DaySales{Date: "2026-11-15", Orders: 1, GrossFils: 200, NetFils: 200}) {
		t.Errorf("days %+v", rep.Days)
	}
	// The sale after midnight counts on its business day, at its own hour.
	if len(rep.Hours) != 24 || rep.Hours[8].GrossFils != 300 || rep.Hours[21].Orders != 2 || rep.Hours[21].GrossFils != 250 ||
		rep.Hours[0].GrossFils != 200 || rep.Hours[12].Orders != 0 || rep.Hours[12].Hour != 12 {
		t.Errorf("hours %+v", rep.Hours)
	}
	// Items group by ID, under the name they last sold as.
	if len(rep.Items) != 2 || rep.Items[0] != (api.ItemSales{ItemId: "karak", Name: "Karak chai", Qty: 4, GrossFils: 450}) ||
		rep.Items[1] != (api.ItemSales{ItemId: "parotta", Name: "Parotta", Qty: 3, GrossFils: 300}) {
		t.Errorf("items %+v", rep.Items)
	}
	if len(rep.Tills) != 1 || rep.Tills[0].Code != "T1" || rep.Tills[0].Name != "Front counter" || rep.Tills[0].Orders != 4 ||
		rep.Tills[0].NetFils != 650 {
		t.Errorf("tills %+v", rep.Tills)
	}
	if len(rep.Staff) != 2 || rep.Staff[0].Name != "Anil" || rep.Staff[0].Orders != 3 || rep.Staff[0].GrossFils != 600 ||
		rep.Staff[1].Name != "Ravi" || rep.Staff[1].GrossFils != 150 {
		t.Errorf("staff %+v", rep.Staff)
	}
	if len(rep.Closes) != 2 {
		t.Fatalf("closes %+v", rep.Closes)
	}
	if c2 := rep.Closes[0]; c2.Date != "2026-11-15" || c2.ClosedAt != nil || c2.CountedFils != nil || c2.DifferenceFils != nil ||
		c2.ExpectedFils != 5000 {
		t.Errorf("the open day %+v", c2)
	}
	if c1 := rep.Closes[1]; c1.Date != "2026-11-14" || c1.TerminalCode != "T1" || c1.CashSalesFils != 400 || c1.CashRefundsFils != 100 ||
		c1.ExpectedFils != 10300 || c1.CountedFils == nil || *c1.CountedFils != 10250 || c1.DifferenceFils == nil || *c1.DifferenceFils != -50 ||
		c1.ClosedByName == nil || *c1.ClosedByName != "Ravi" {
		t.Errorf("the closed day %+v", c1)
	}

	// One day, one shop.
	c.do("GET", "/v1/admin/reports/sales?from=2026-11-15&to=2026-11-15&shopId="+shop.Id.String(), nil, &rep)
	if rep.Totals.Orders != 1 || len(rep.Days) != 1 || len(rep.Closes) != 1 || len(rep.Items) != 1 {
		t.Errorf("one day: %+v", rep)
	}
	// A range with no sales has its days, at nothing.
	c.do("GET", "/v1/admin/reports/sales?from=2026-10-01&to=2026-10-03", nil, &rep)
	if rep.Totals != (api.SalesTotals{}) || len(rep.Days) != 3 || len(rep.Items) != 0 || len(rep.Closes) != 0 {
		t.Errorf("no sales: %+v", rep)
	}

	var e api.Error
	for q, code := range map[string]int{
		"from=2026-11-15&to=2026-11-14":                            400,
		"from=2025-11-01&to=2026-11-14":                            400,
		"from=14-11-2026&to=2026-11-14":                            400,
		"from=2026-11-14":                                          400,
		"from=2026-11-14&to=2026-11-14&shopId=" + uuid.NewString(): 404,
	} {
		if got := c.do("GET", "/v1/admin/reports/sales?"+q, nil, &e); got != code {
			t.Errorf("%s: %d, want %d", q, got, code)
		}
	}
	if code := newClient(t, c.ts).do("GET", "/v1/admin/reports/sales?from=2026-11-14&to=2026-11-14", nil, nil); code != 401 {
		t.Errorf("signed out: %d", code)
	}
	// Another organization sees none of it, and not this one's shop.
	if err := otherOrg(c.pool); err != nil {
		t.Fatal(err)
	}
	other := newClient(t, c.ts)
	other.do("POST", "/v1/admin/session", api.SignIn{Username: "other", Password: ownerPassword}, nil)
	if code := other.do("GET", "/v1/admin/reports/sales?from=2026-11-14&to=2026-11-15", nil, &rep); code != 200 ||
		rep.Totals.Orders != 0 || len(rep.Closes) != 0 || len(rep.Tills) != 0 || len(rep.Staff) != 0 {
		t.Errorf("another organization: %d %+v", code, rep)
	}
	if code := other.do("GET", "/v1/admin/reports/sales?from=2026-11-14&to=2026-11-15&shopId="+shop.Id.String(), nil, nil); code != 404 {
		t.Errorf("another organization's shop: %d", code)
	}
}

// Profit counts each sale at the cost its item had when it was sold: an
// item's first cost reaches back to its earlier sales, a change only
// forward. Options add their cost; items without one stay out of it.
func TestProfit(t *testing.T) {
	c, _, _ := newServer(t)
	_, token, manager, _ := tillSetup(t, c)
	till := newClient(t, c.ts)
	var cat api.Catalog
	c.do("GET", "/v1/admin/catalog", nil, &cat)
	karak := cat.Items[0]

	var extras api.ModifierGroup
	if code := c.do("POST", "/v1/admin/modifier-groups", api.ModifierGroupInput{Name: "Extras", Multiple: true,
		Modifiers: []api.ModifierInput{{Name: "Extra milk", PriceDeltaFils: 50, CostFils: ptr(int64(20))}}}, &extras); code != 201 ||
		extras.Modifiers[0].CostFils == nil || *extras.Modifiers[0].CostFils != 20 {
		t.Fatalf("an option with a cost: %d %+v", code, extras)
	}
	saveKarak := func(cost *int64) int {
		var e api.Error
		return c.do("PUT", "/v1/admin/items/"+karak.Id.String(), api.ItemInput{CategoryId: karak.CategoryId, Name: karak.Name,
			PriceFils: karak.PriceFils, ModifierGroupIds: []uuid.UUID{extras.Id}, CostFils: cost}, &e)
	}
	if code := saveKarak(ptr(int64(-1))); code != 400 {
		t.Errorf("a cost below nothing: %d", code)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	date := now.Format("2006-01-02")
	day := uuid.New()
	line := func(item string, qty int, unit int64, mods ...api.OrderLineModifierEvent) api.OrderLineEvent {
		if mods == nil {
			mods = []api.OrderLineModifierEvent{}
		}
		var delta int64
		for _, m := range mods {
			delta += m.PriceDeltaFils
		}
		return api.OrderLineEvent{Id: uuid.New(), ItemId: item, Name: item, Qty: qty, UnitPriceFils: unit + delta,
			LineTotalFils: int64(qty) * (unit + delta), Modifiers: mods}
	}
	order := func(n int, at time.Time, lines ...api.OrderLineEvent) api.OrderEvent {
		var total int64
		for _, l := range lines {
			total += l.LineTotalFils
		}
		return api.OrderEvent{Id: uuid.New(), BusinessDayId: day, Number: n, Code: "T1-00" + itoa(int64(n)), CreatedAt: at,
			StaffId: manager.Id, StaffName: "Ravi", TotalFils: total, PaymentMethod: api.Cash, TenderedFils: total, Lines: lines}
	}
	milk := api.OrderLineModifierEvent{GroupId: extras.Id.String(), GroupName: "Extras",
		ModifierId: extras.Modifiers[0].Id.String(), Name: "Extra milk", PriceDeltaFils: 50}
	// Sold before karak had a cost: 2 with extra milk, and a parotta from the demo menu.
	early := order(1, now.Add(-2*time.Hour), line(karak.Id.String(), 2, 100, milk), line("parotta", 1, 100))
	send := func(seq int64, ev api.Event) {
		var res api.EventResult
		ev.Seq = seq
		till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{ev}}, &res, bearer(token)...)
		if len(res.Accepted) != 1 {
			t.Fatalf("event %d: %+v", seq, res)
		}
	}
	send(1, api.Event{Type: api.EventTypeBusinessDay, BusinessDay: &api.BusinessDayEvent{Id: day, Date: date,
		OpenedAt: now.Add(-3 * time.Hour), OpenedBy: manager.Id, OpenedByName: "Ravi", OpeningFloatFils: 0}})
	send(2, api.Event{Type: api.EventTypeOrder, Order: &early})

	// Its first cost reaches back; a change, later, doesn't.
	if code := saveKarak(ptr(int64(40))); code != 200 {
		t.Fatalf("a first cost: %d", code)
	}
	time.Sleep(20 * time.Millisecond)
	if code := saveKarak(ptr(int64(60))); code != 200 {
		t.Fatalf("a new cost: %d", code)
	}
	time.Sleep(20 * time.Millisecond)
	late := order(2, time.Now().UTC(), line(karak.Id.String(), 1, 100))
	send(3, api.Event{Type: api.EventTypeOrder, Order: &late})
	send(4, api.Event{Type: api.EventTypeRefund, Refund: &api.RefundEvent{Id: uuid.New(), OrderId: early.Id, BusinessDayId: day,
		CreatedAt: now, StaffId: manager.Id, StaffName: "Ravi", ApprovedBy: manager.Id, ApprovedByName: "Ravi",
		Reason: "Spilt", Method: api.Cash, TotalFils: 150,
		Lines: []api.RefundLineEvent{{OrderLineId: early.Lines[0].Id, Qty: 1, AmountFils: 150}}}})

	var rep api.SalesReport
	c.do("GET", "/v1/admin/reports/sales?from="+date+"&to="+date, nil, &rep)
	// Cost: 2 × (40 + 20) early, 1 × 60 late. Sales with a cost: 300 + 100,
	// less 150 refunded. The parotta, 100, has no cost.
	if rep.Totals.CostFils != 180 || rep.Totals.ProfitFils != 400-150-180 || rep.Totals.UncostedFils != 100 {
		t.Errorf("totals %+v", rep.Totals)
	}
	for _, it := range rep.Items {
		switch it.ItemId {
		case karak.Id.String():
			if it.CostFils == nil || *it.CostFils != 180 {
				t.Errorf("karak's cost: %v", it.CostFils)
			}
		case "parotta":
			if it.CostFils != nil {
				t.Errorf("the parotta has a cost: %d", *it.CostFils)
			}
		}
	}

	// The menu shows today's costs, and clearing one leaves later sales without.
	c.do("GET", "/v1/admin/catalog", nil, &cat)
	if cat.Items[0].CostFils == nil || *cat.Items[0].CostFils != 60 || cat.ModifierGroups[0].Modifiers[0].CostFils == nil {
		t.Errorf("the menu's costs: %+v %+v", cat.Items[0], cat.ModifierGroups[0].Modifiers[0])
	}
	time.Sleep(20 * time.Millisecond)
	saveKarak(nil)
	time.Sleep(20 * time.Millisecond)
	c.do("GET", "/v1/admin/catalog", nil, &cat)
	if cat.Items[0].CostFils != nil {
		t.Errorf("a cleared cost: %d", *cat.Items[0].CostFils)
	}
	cleared := order(3, time.Now().UTC(), line(karak.Id.String(), 1, 100))
	send(5, api.Event{Type: api.EventTypeOrder, Order: &cleared})
	c.do("GET", "/v1/admin/reports/sales?from="+date+"&to="+date, nil, &rep)
	if rep.Totals.CostFils != 180 || rep.Totals.UncostedFils != 200 {
		t.Errorf("after clearing: %+v", rep.Totals)
	}
	// The tills never hear of costs.
	var ch api.Changes
	till.do("GET", "/v1/terminal/changes?since=0", nil, &ch, bearer(token)...)
	if b, _ := json.Marshal(ch); strings.Contains(string(b), "cost") {
		t.Error("costs reach the tills")
	}
}

// TestCashCarriedOver: a day that closed by itself, without a count, and
// cash taken out of its drawer, as a till sends them.
func TestCashCarriedOver(t *testing.T) {
	c, _, _ := newServer(t)
	_, token, manager, _ := tillSetup(t, c)
	till := newClient(t, c.ts)
	now := time.Now().UTC().Truncate(time.Millisecond)
	date := now.Format("2006-01-02")
	day := uuid.New()
	send := func(seq int64, ev api.Event) {
		var res api.EventResult
		ev.Seq = seq
		till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{ev}}, &res, bearer(token)...)
		if len(res.Accepted) != 1 {
			t.Fatalf("event %d: %+v", seq, res)
		}
	}
	open := api.BusinessDayEvent{Id: day, Date: date, OpenedAt: now.Add(-3 * time.Hour), OpenedBy: manager.Id,
		OpenedByName: "Ravi", OpeningFloatFils: 30000}
	send(1, api.Event{Type: api.EventTypeBusinessDay, BusinessDay: &open})
	sale := api.OrderEvent{Id: uuid.New(), BusinessDayId: day, Number: 1, Code: "T1-001", CreatedAt: now.Add(-2 * time.Hour),
		StaffId: manager.Id, StaffName: "Ravi", TotalFils: 1500, PaymentMethod: api.Cash, TenderedFils: 1500,
		Lines: []api.OrderLineEvent{{Id: uuid.New(), ItemId: "karak", Name: "Karak chai", Qty: 15, UnitPriceFils: 100,
			LineTotalFils: 1500, Modifiers: []api.OrderLineModifierEvent{}}}}
	send(2, api.Event{Type: api.EventTypeOrder, Order: &sale})
	out := api.CashMovementEvent{Id: uuid.New(), BusinessDayId: day, CreatedAt: now.Add(-time.Hour), StaffId: manager.Id,
		StaffName: "Ravi", ApprovedBy: manager.Id, ApprovedByName: "Ravi", AmountFils: -20000, Reason: "Banked"}
	send(3, api.Event{Type: api.EventTypeCashMovement, CashMovement: &out})
	send(4, api.Event{Type: api.EventTypeCashMovement, CashMovement: &out}) // sent again: taken once
	closed := open
	closed.ClosedAt = &now // by itself: no closedBy, and no count
	send(5, api.Event{Type: api.EventTypeBusinessDay, BusinessDay: &closed})

	var rep api.SalesReport
	c.do("GET", "/v1/admin/reports/sales?from="+date+"&to="+date, nil, &rep)
	if len(rep.Closes) != 1 {
		t.Fatalf("closes %+v", rep.Closes)
	}
	cl := rep.Closes[0]
	if cl.CashMovementsFils != -20000 || cl.ExpectedFils != 30000+1500-20000 || cl.ClosedAt == nil ||
		cl.ClosedByName != nil || cl.CountedFils != nil || cl.DifferenceFils != nil {
		t.Errorf("close %+v", cl)
	}
	if len(rep.CashMovements) != 1 || rep.CashMovements[0].AmountFils != -20000 || rep.CashMovements[0].Reason != "Banked" ||
		rep.CashMovements[0].TerminalCode != "T1" {
		t.Errorf("cash movements %+v", rep.CashMovements)
	}

	// A movement needs an amount, and a day the till sent.
	var res api.EventResult
	zero := out
	zero.Id, zero.AmountFils = uuid.New(), 0
	till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{
		{Seq: 6, Type: api.EventTypeCashMovement, CashMovement: &zero}}}, &res, bearer(token)...)
	if res.Rejected == nil || res.Rejected.Seq != 6 {
		t.Errorf("a movement of nothing: %+v", res)
	}
}
