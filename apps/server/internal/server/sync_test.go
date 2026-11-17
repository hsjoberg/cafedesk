package server_test

import (
	"context"
	"testing"
	"time"

	"cafedesk/apps/server/internal/api"
	"cafedesk/apps/server/internal/auth"

	"github.com/google/uuid"
)

func ptr[T any](v T) *T { return &v }

// tillSetup signs in, adds a manager and a cashier, a category with an
// item, and a paired till. It returns the shop and the till's token.
func tillSetup(t *testing.T, c *client) (shop api.Shop, token string, manager, cashier api.Staff) {
	t.Helper()
	c.signIn()
	var me api.Me
	c.do("GET", "/v1/admin/me", nil, &me)
	shop = me.Shops[0]

	var e api.Error
	if code := c.do("POST", "/v1/admin/terminals", api.TerminalInput{ShopId: shop.Id}, &e); code != 409 {
		t.Fatalf("a till without a manager: %d %+v", code, e)
	}
	if code := c.do("POST", "/v1/admin/staff", api.StaffInput{Name: "Ravi", Role: api.StaffRoleManager,
		ShopIds: []uuid.UUID{shop.Id}, Active: true, Pin: ptr("5678")}, &manager); code != 201 || !manager.MustChangePin {
		t.Fatalf("manager: %d %+v", code, manager)
	}
	if code := c.do("POST", "/v1/admin/staff", api.StaffInput{Name: "Anil", Role: api.StaffRoleCashier,
		ShopIds: []uuid.UUID{shop.Id}, Active: true, Pin: ptr("5678")}, &e); code != 400 || e.Error.Message != "Someone else has that PIN. Choose another." {
		t.Fatalf("a taken PIN: %d %+v", code, e)
	}
	c.do("POST", "/v1/admin/staff", api.StaffInput{Name: "Anil", Role: api.StaffRoleCashier,
		ShopIds: []uuid.UUID{shop.Id}, Active: true, Pin: ptr("2468")}, &cashier)

	var tea api.Category
	c.do("POST", "/v1/admin/categories", api.CategoryInput{Name: "Tea", Color: "#5F8A3A"}, &tea)
	c.do("POST", "/v1/admin/items", api.ItemInput{CategoryId: tea.Id, Name: "Karak chai", PriceFils: 100,
		ModifierGroupIds: []uuid.UUID{}}, nil)

	var p api.TerminalPairing
	if code := c.do("POST", "/v1/admin/terminals", api.TerminalInput{ShopId: shop.Id, Name: ptr("Front counter")}, &p); code != 201 ||
		len(p.Code) != 8 || p.Terminal.Code != "T1" || p.Terminal.Status != api.Waiting {
		t.Fatalf("till: %d %+v", code, p)
	}
	if code := c.do("POST", "/v1/terminal/pair", api.PairRequest{Code: "00000000"}, &e); code != 400 || e.Error.Field == nil {
		t.Fatalf("a wrong code: %d %+v", code, e)
	}
	var pairing api.Pairing
	if code := c.do("POST", "/v1/terminal/pair", api.PairRequest{Code: p.Code[:4] + " " + p.Code[4:]}, &pairing); code != 200 ||
		pairing.Token == "" || pairing.Shop.Id != shop.Id || pairing.Terminal.Code != "T1" {
		t.Fatalf("pair: %d %+v", code, pairing)
	}
	if code := c.do("POST", "/v1/terminal/pair", api.PairRequest{Code: p.Code}, nil); code != 400 {
		t.Fatalf("a code used twice: %d", code)
	}
	return shop, pairing.Token, manager, cashier
}

func bearer(token string) []string { return []string{"Authorization", "Bearer " + token} }

func TestPairingAndChanges(t *testing.T) {
	c, _, _ := newServer(t)
	_, token, manager, cashier := tillSetup(t, c)
	till := newClient(t, c.ts) // a till: no session cookie

	var ch api.Changes
	if code := till.do("GET", "/v1/terminal/changes?since=0", nil, &ch, bearer(token)...); code != 200 {
		t.Fatalf("changes: %d", code)
	}
	if !ch.Full || len(ch.Categories) != 1 || len(ch.Items) != 1 || len(ch.Staff) != 2 {
		t.Fatalf("full changes: %+v", ch)
	}
	for _, st := range ch.Staff {
		if st.Id == manager.Id && (!auth.CheckPIN(st.PinHash, "5678") || !st.MustChangePin || st.Role != api.StaffRoleManager) {
			t.Errorf("manager on the till: %+v", st)
		}
	}

	// A change to one item brings that item alone.
	var cat api.Catalog
	c.do("GET", "/v1/admin/catalog", nil, &cat)
	item := cat.Items[0]
	c.do("PUT", "/v1/admin/items/"+item.Id.String(), api.ItemInput{CategoryId: item.CategoryId, Name: item.Name,
		PriceFils: 150, ModifierGroupIds: []uuid.UUID{}}, nil)
	var next api.Changes
	till.do("GET", "/v1/terminal/changes?since="+itoa(ch.Version), nil, &next, bearer(token)...)
	if next.Full || len(next.Items) != 1 || next.Items[0].PriceFils != 150 || len(next.Categories) != 0 || len(next.Staff) != 0 {
		t.Fatalf("changes since %d: %+v", ch.Version, next)
	}

	// Deleting reaches the till as archived; so does turning someone off.
	c.do("DELETE", "/v1/admin/items/"+item.Id.String(), nil, nil)
	c.do("PUT", "/v1/admin/staff/"+cashier.Id.String(), api.StaffInput{Name: "Anil", Role: api.StaffRoleCashier,
		ShopIds: cashier.ShopIds, Active: false}, nil)
	var last api.Changes
	till.do("GET", "/v1/terminal/changes?since="+itoa(next.Version), nil, &last, bearer(token)...)
	if len(last.Items) != 1 || !last.Items[0].Archived || len(last.Staff) != 1 || last.Staff[0].Active {
		t.Fatalf("deletions: %+v", last)
	}

	if code := till.do("GET", "/v1/terminal/changes?since=0", nil, nil); code != 401 {
		t.Errorf("no token: %d", code)
	}
	if code := till.do("GET", "/v1/terminal/changes?since=0", nil, nil, bearer("not-a-token")...); code != 401 {
		t.Errorf("a wrong token: %d", code)
	}
	// The till shows as paired and seen.
	var tills []api.Terminal
	c.do("GET", "/v1/admin/terminals", nil, &tills)
	if len(tills) != 1 || tills[0].Status != api.Paired || tills[0].LastSeenAt == nil || tills[0].Name != "Front counter" {
		t.Errorf("tills %+v", tills)
	}
	// Unpairing ends the token at once.
	c.do("DELETE", "/v1/admin/terminals/"+tills[0].Id.String(), nil, nil)
	if code := till.do("GET", "/v1/terminal/changes?since=0", nil, nil, bearer(token)...); code != 401 {
		t.Errorf("an unpaired till: %d", code)
	}
	c.do("GET", "/v1/admin/terminals", nil, &tills)
	if tills[0].Status != api.Unpaired {
		t.Errorf("status after unpairing: %s", tills[0].Status)
	}
	// A new code pairs it again.
	var p api.TerminalPairing
	c.do("POST", "/v1/admin/terminals/"+tills[0].Id.String()+"/pairing-code", nil, &p)
	if code := till.do("POST", "/v1/terminal/pair", api.PairRequest{Code: p.Code}, nil); code != 200 {
		t.Errorf("pairing again: %d", code)
	}
}

func itoa(n int64) string { return fmtInt(n) }

func TestEvents(t *testing.T) {
	c, _, _ := newServer(t)
	_, token, manager, _ := tillSetup(t, c)
	till := newClient(t, c.ts)
	now := time.Now().UTC().Truncate(time.Millisecond)

	day := uuid.New()
	order := api.OrderEvent{Id: uuid.New(), BusinessDayId: day, Number: 1, Code: "T1-001", CreatedAt: now,
		StaffId: manager.Id, StaffName: "Ravi", TotalFils: 300, PaymentMethod: api.Cash, TenderedFils: 500, ChangeFils: 200,
		Lines: []api.OrderLineEvent{
			{Id: uuid.New(), ItemId: "karak", Name: "Karak chai", Qty: 2, UnitPriceFils: 100, LineTotalFils: 200,
				Modifiers: []api.OrderLineModifierEvent{{GroupId: "sugar", GroupName: "Sugar", ModifierId: "sugar-less", Name: "Less sugar"}}},
			{Id: uuid.New(), ItemId: "parotta", Name: "Parotta", Qty: 1, UnitPriceFils: 100, LineTotalFils: 100, Kitchen: true,
				Modifiers: []api.OrderLineModifierEvent{}},
		}}
	dayEvent := api.BusinessDayEvent{Id: day, Date: now.Format("2006-01-02"), OpenedAt: now, OpenedBy: manager.Id,
		OpenedByName: "Ravi", OpeningFloatFils: 10000}

	// An order before its day is turned away, and nothing after it is taken.
	var res api.EventResult
	till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{
		{Seq: 1, Type: api.EventTypeOrder, Order: &order},
	}}, &res, bearer(token)...)
	if len(res.Accepted) != 0 || res.Rejected == nil || res.Rejected.Seq != 1 {
		t.Fatalf("an order before its day: %+v", res)
	}

	var start api.Changes
	till.do("GET", "/v1/terminal/changes?since=0", nil, &start, bearer(token)...)
	startingHash := ""
	for _, st := range start.Staff {
		if st.Id == manager.Id {
			startingHash = st.PinHash
		}
	}
	newPIN := auth.HashPIN("9090")
	batch := api.EventBatch{Events: []api.Event{
		{Seq: 1, Type: api.EventTypeBusinessDay, BusinessDay: &dayEvent},
		{Seq: 2, Type: api.EventTypeOrder, Order: &order},
		{Seq: 3, Type: api.EventTypeRefund, Refund: &api.RefundEvent{Id: uuid.New(), OrderId: order.Id, BusinessDayId: day, CreatedAt: now,
			StaffId: manager.Id, StaffName: "Ravi", ApprovedBy: manager.Id, ApprovedByName: "Ravi", Reason: "Other",
			Method: api.Cash, TotalFils: 100, Lines: []api.RefundLineEvent{{OrderLineId: order.Lines[0].Id, Qty: 1, AmountFils: 100}}}},
		{Seq: 4, Type: api.EventTypeStaffPin, StaffPin: &api.StaffPinEvent{StaffId: manager.Id, PreviousPinHash: startingHash, PinHash: newPIN, ChangedAt: now}},
	}}
	for round := 0; round < 2; round++ { // sending again changes nothing
		res = api.EventResult{}
		if code := till.do("POST", "/v1/terminal/events", batch, &res, bearer(token)...); code != 200 || len(res.Accepted) != 4 || res.Rejected != nil {
			t.Fatalf("round %d: %d %+v", round, code, res)
		}
	}
	closed := dayEvent
	closed.ClosedAt, closed.ClosedBy, closed.ClosedByName, closed.CountedCashFils = ptr(now.Add(time.Hour)), &manager.Id, ptr("Ravi"), ptr(int64(10100))
	till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{
		{Seq: 5, Type: api.EventTypeOrderVoid, OrderVoid: &api.OrderVoidEvent{OrderId: order.Id, VoidedAt: now, VoidedBy: manager.Id,
			VoidedByName: "Ravi", Reason: "Test order"}},
		{Seq: 6, Type: api.EventTypeBusinessDay, BusinessDay: &closed},
	}}, &res, bearer(token)...)
	if len(res.Accepted) != 2 {
		t.Fatalf("void and close: %+v", res)
	}

	ctx := context.Background()
	var orders, lines, mods, refunds int
	var status string
	var counted int64
	c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM orders").Scan(&orders)
	c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM order_lines").Scan(&lines)
	c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM order_line_modifiers").Scan(&mods)
	c.pool.QueryRow(ctx, "SELECT COUNT(*) FROM refunds").Scan(&refunds)
	c.pool.QueryRow(ctx, "SELECT status FROM orders").Scan(&status)
	c.pool.QueryRow(ctx, "SELECT counted_cash_fils FROM business_days").Scan(&counted)
	if orders != 1 || lines != 2 || mods != 1 || refunds != 1 || status != "voided" || counted != 10100 {
		t.Errorf("saved: %d orders, %d lines, %d modifiers, %d refunds, status %s, counted %d",
			orders, lines, mods, refunds, status, counted)
	}

	// The PIN chosen at the till goes to the other tills: the next changes
	// carry it, no longer a starting PIN.
	var ch api.Changes
	till.do("GET", "/v1/terminal/changes?since=0", nil, &ch, bearer(token)...)
	for _, st := range ch.Staff {
		if st.Id == manager.Id && (st.PinHash != newPIN || st.MustChangePin) {
			t.Errorf("the PIN chosen at the till: %+v", st)
		}
	}
	// A starting PIN set in the admin afterwards wins over an older one sent late.
	var e api.Error
	if code := c.do("PUT", "/v1/admin/staff/"+manager.Id.String(), api.StaffInput{Name: "Ravi", Role: api.StaffRoleManager,
		ShopIds: manager.ShopIds, Active: true, Pin: ptr("1357")}, &e); code != 200 {
		t.Fatalf("a new starting PIN: %d %+v", code, e)
	}
	till.do("POST", "/v1/terminal/events", api.EventBatch{Events: []api.Event{
		{Seq: 7, Type: api.EventTypeStaffPin, StaffPin: &api.StaffPinEvent{StaffId: manager.Id, PreviousPinHash: newPIN, PinHash: auth.HashPIN("1111"), ChangedAt: now}},
	}}, &res, bearer(token)...)
	till.do("GET", "/v1/terminal/changes?since=0", nil, &ch, bearer(token)...)
	for _, st := range ch.Staff {
		if st.Id == manager.Id && (!auth.CheckPIN(st.PinHash, "1357") || !st.MustChangePin) {
			t.Errorf("a late PIN from the till overwrote the admin's: %+v", st)
		}
	}
}
