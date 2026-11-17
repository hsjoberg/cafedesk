import { createEffect, createSignal, For, on, Show } from "solid-js";
import { api, call, type OrderList, type OrderRow } from "../api/client";
import { formatAmount } from "../money";

/** The till's void reason for an order rung up to try the till: not counted at all. */
const testOrder = "Test order";

/**
 * The orders behind the figures, one by one, newest first: to look one up,
 * or tell test orders from real ones. Loaded when asked, as a month can be
 * thousands.
 */
export function OrdersPanel(props: { from: string; to: string; shopId?: string; timeZone: string; oneDay: boolean }) {
  const [open, setOpen] = createSignal(false);
  const [list, setList] = createSignal<OrderList>();
  const [error, setError] = createSignal("");
  const [loading, setLoading] = createSignal(false);
  let asked = 0;
  createEffect(
    on([open, () => props.from, () => props.to, () => props.shopId], async ([o, from, to, shopId]) => {
      if (!o) return;
      const mine = ++asked;
      setLoading(true);
      try {
        const r = await call(api.GET("/v1/admin/reports/orders", { params: { query: { from, to, shopId } } }));
        if (mine === asked) {
          setList(r);
          setError("");
        }
      } catch (e) {
        if (mine === asked) setError((e as Error).message);
      } finally {
        if (mine === asked) setLoading(false);
      }
    }),
  );

  const time = (o: OrderRow) =>
    new Intl.DateTimeFormat("en-GB", {
      timeZone: props.timeZone,
      hour: "2-digit",
      minute: "2-digit",
      ...(props.oneDay ? {} : { weekday: "short", day: "numeric", month: "short" }),
    }).format(new Date(o.createdAt));
  const items = (o: OrderRow) =>
    o.lines.map((l) => `${l.qty} × ${l.name}${l.options.length ? ` (${l.options.join(", ")})` : ""}`).join(", ");
  const voided = () => list()?.orders.filter((o) => o.status === "voided") ?? [];
  const tests = () => voided().filter((o) => o.voidReason === testOrder).length;
  const summary = () => {
    const l = list();
    if (!l) return "";
    const n = l.orders.length;
    let s = `${n === 1 ? "1 order" : `${n} orders`}${l.more ? " (the newest; choose fewer days for all)" : ""}`;
    if (voided().length) {
      s += `, ${voided().length} voided`;
      if (tests()) s += ` (${tests()} as test orders, which no figure counts)`;
    }
    return `${s}.`;
  };

  return (
    <section class="panel orders">
      <div class="panel-head">
        <h2>Orders</h2>
        <Show when={open() && list()}>
          <span class="meta">{summary()}</span>
        </Show>
      </div>
      <Show
        when={open()}
        fallback={
          <>
            <p class="meta">Each order on its own: when, on which till, who took it, what was in it, and whether it was voided or refunded.</p>
            <button type="button" class="btn btn-option" onClick={() => setOpen(true)}>
              Show the orders
            </button>
          </>
        }
      >
        <Show when={error()}>
          <p class="banner" role="alert">{error()}</p>
        </Show>
        <Show when={list()} fallback={<p class="loading">{loading() ? "Getting the orders." : ""}</p>}>
          {(l) => (
            <Show when={l().orders.length} fallback={<p class="meta">No orders on these dates.</p>}>
              <div class="table-wrap">
                <table class="table order-table">
                  <thead>
                    <tr>
                      <th scope="col">Time</th>
                      <th scope="col">Order</th>
                      <th scope="col">Taken by</th>
                      <th scope="col">What</th>
                      <th scope="col">Paid</th>
                      <th scope="col" class="n">Total</th>
                    </tr>
                  </thead>
                  <tbody>
                    <For each={l().orders}>
                      {(o) => (
                        <tr classList={{ voided: o.status === "voided" }}>
                          <td class="nowrap">{time(o)}</td>
                          <th scope="row" class="nowrap">{o.code}</th>
                          <td>{o.staffName}</td>
                          <td class="what">
                            {items(o)}
                            <Show when={o.status === "voided"}>
                              <span class="meta status">
                                {o.voidReason === testOrder ? "Test order" : "Voided"}
                                {o.voidedByName ? ` by ${o.voidedByName}` : ""}
                                {o.voidReason && o.voidReason !== testOrder ? `: ${o.voidReason}` : ""}. Not counted.
                              </span>
                            </Show>
                            <Show when={o.refundedFils > 0}>
                              <span class="meta status">Refunded {formatAmount(o.refundedFils)}.</span>
                            </Show>
                          </td>
                          <td>{o.paymentMethod === "cash" ? "Cash" : "Card"}</td>
                          <td class="n">{formatAmount(o.totalFils)}</td>
                        </tr>
                      )}
                    </For>
                  </tbody>
                </table>
              </div>
            </Show>
          )}
        </Show>
      </Show>
    </section>
  );
}
