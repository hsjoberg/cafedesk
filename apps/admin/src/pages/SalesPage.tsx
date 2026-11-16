import { A, useSearchParams } from "@solidjs/router";
import { createEffect, createMemo, createSignal, For, on, onMount, Show } from "solid-js";
import { api, call, type DayClose, type SalesReport, type Terminal } from "../api/client";
import { ColumnChart, type Column } from "../components/ColumnChart";
import { OverdueInvoices } from "../components/Overdue";
import { daysBetween, isDate, longDate, presets, rangeLabel, shortDate, todayIn } from "../dates";
import { formatAmount } from "../money";
import { me } from "../state";

const hourName = (h: number) =>
  new Date(Date.UTC(2000, 0, 1, h)).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit", timeZone: "UTC" });
/** Profit as a share of sales, as "58%". */
const margin = (profit: number, sales: number) => (sales > 0 ? `${Math.round((profit / sales) * 100)}%` : "");
const orders = (n: number) => (n === 1 ? "1 order" : `${n} orders`);
const when = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
const time = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });

/** A till is behind when it hasn't been in touch for this long. */
const staleAfter = 15 * 60_000;

/** Sales: what the shops took, and whether the cash adds up. */
export function SalesPage() {
  const [params, setParams] = useSearchParams<{ from?: string; to?: string; shop?: string; view?: string }>();
  const shops = () => me()?.shops ?? [];
  const shop = () => shops().find((s) => s.id === params.shop);
  const timeZone = () => (shop() ?? shops()[0])?.timezone ?? "Asia/Dubai";
  const today = () => todayIn(timeZone());

  const from = () => (isDate(params.from) ? params.from : today());
  const to = () => (isDate(params.to) && params.to >= from() ? params.to : from() > today() ? from() : today());
  const oneDay = () => from() === to();
  const preset = () => presets.find((p) => {
    const [f, t] = p.range(today());
    return f === from() && t === to();
  });

  const [report, setReport] = createSignal<SalesReport>();
  const [loading, setLoading] = createSignal(false);
  const [error, setError] = createSignal("");
  const [tills, setTills] = createSignal<Terminal[]>([]);
  let asked = 0;
  createEffect(
    on([from, to, () => params.shop], async ([f, t, s]) => {
      const mine = ++asked;
      setLoading(true);
      try {
        const query = { from: f, to: t, shopId: shop() ? s : undefined };
        const r = await call(api.GET("/v1/admin/reports/sales", { params: { query } }));
        if (mine === asked) {
          setReport(r);
          setError("");
        }
      } catch (e) {
        if (mine === asked) setError((e as Error).message);
      } finally {
        if (mine === asked) setLoading(false);
      }
    }),
  );
  onMount(async () => {
    try {
      setTills(await call(api.GET("/v1/admin/terminals")));
    } catch {
      // Only for the note on tills that are behind.
    }
  });
  const behind = () =>
    tills().filter(
      (t) =>
        t.status === "paired" &&
        (!shop() || t.shopId === shop()!.id) &&
        (!t.lastSeenAt || Date.now() - Date.parse(t.lastSeenAt) > staleAfter),
    );

  const byHour = () => oneDay() || params.view === "hours";
  const columns = createMemo<Column[]>(() => {
    const r = report();
    if (!r) return [];
    if (byHour()) {
      const busy = r.hours.filter((h) => h.orders > 0).map((h) => h.hour);
      if (!busy.length) return [];
      const first = Math.max(0, Math.min(...busy) - 1);
      const last = Math.min(23, Math.max(...busy) + 1);
      return r.hours.slice(first, last + 1).map((h) => ({
        label: `${hourName(h.hour)} to ${hourName((h.hour + 1) % 24)}`,
        tick: hourName(h.hour),
        value: h.grossFils,
        detail: orders(h.orders),
      }));
    }
    return r.days.map((d) => ({
      label: longDate(d.date, today()),
      tick: String(Number(d.date.slice(8))),
      value: d.grossFils,
      detail: orders(d.orders),
    }));
  });

  const pick = (range: [string, string]) => setParams({ from: range[0], to: range[1] });
  const setFrom = (v: string) => {
    if (!isDate(v)) return;
    setParams({ from: v, to: to() < v ? v : to() });
  };
  const setTo = (v: string) => {
    if (!isDate(v)) return;
    setParams({ to: v, from: from() > v ? v : from() });
  };
  const tooLong = () => daysBetween(from(), to()) > 366;

  const [asTable, setAsTable] = createSignal(false);
  const [allItems, setAllItems] = createSignal(false);
  const items = () => {
    const list = report()?.items ?? [];
    return allItems() ? list : list.slice(0, 10);
  };
  const topItem = () => Math.max(...(report()?.items ?? []).map((i) => i.grossFils), 1);
  const shortCount = () => (report()?.closes ?? []).filter((c) => (c.differenceFils ?? 0) < 0).length;
  // Over a week or more, the counts that came out exact fold away: the
  // owner looks for the ones that didn't.
  const [allCloses, setAllCloses] = createSignal(false);
  const needsLook = (c: DayClose) => c.differenceFils !== 0;
  const closes = () => {
    const list = report()?.closes ?? [];
    return allCloses() || list.length <= 6 ? list : list.filter(needsLook);
  };
  const exactHidden = () => (report()?.closes ?? []).length - closes().length;

  return (
    <div class="workspace sales">
      <section class="wall" aria-label="Sales">
        <div class="wall-head">
          <h1>Sales</h1>
        </div>
        <OverdueInvoices />
        <div class="filters" role="group" aria-label="Dates">
          <For each={presets}>
            {(p) => (
              <button type="button" class="chip" aria-pressed={preset()?.id === p.id} onClick={() => pick(p.range(today()))}>
                {p.label}
              </button>
            )}
          </For>
          <span class="dates">
            <label>
              <span class="visually-hidden">From</span>
              <input type="date" class="input date" value={from()} max={today()} onChange={(e) => setFrom(e.currentTarget.value)} />
            </label>
            <span class="muted">to</span>
            <label>
              <span class="visually-hidden">To</span>
              <input type="date" class="input date" value={to()} max={today()} onChange={(e) => setTo(e.currentTarget.value)} />
            </label>
          </span>
          <Show when={shops().length > 1}>
            <select class="input shop" value={params.shop ?? ""} onChange={(e) => setParams({ shop: e.currentTarget.value || undefined })}>
              <option value="">All shops</option>
              <For each={shops()}>{(s) => <option value={s.id}>{s.name}</option>}</For>
            </select>
          </Show>
        </div>

        <Show when={tooLong()}>
          <p class="banner" role="alert">Choose at most a year at a time.</p>
        </Show>
        <Show when={error() && !tooLong()}>
          <p class="banner" role="alert">{error()}</p>
        </Show>

        <Show when={report()}>
          {(r) => (
            <div class="report" classList={{ stale: loading() }} aria-busy={loading()}>
              <section class="panel">
                <div class="panel-head">
                  <h2>{byHour() ? "Sales by hour" : "Sales by day"}</h2>
                  <span class="spacer" />
                  <Show when={!oneDay()}>
                    <div class="segmented" role="group" aria-label="Show sales">
                      <button type="button" aria-pressed={!byHour()} onClick={() => setParams({ view: undefined })}>
                        By day
                      </button>
                      <button type="button" aria-pressed={byHour()} onClick={() => setParams({ view: "hours" })}>
                        By hour
                      </button>
                    </div>
                  </Show>
                </div>
                <Show when={!oneDay() && byHour()}>
                  <p class="meta">Each hour of the clock, added up over the {daysBetween(from(), to())} days.</p>
                </Show>
                <Show
                  when={r().totals.orders > 0}
                  fallback={
                    <p class="empty">
                      No sales on {oneDay() ? "this day" : "these dates"}. Tills send their sales whenever they are online.
                    </p>
                  }
                >
                  <Show
                    when={asTable()}
                    fallback={<ColumnChart columns={columns()} label={byHour() ? "Sales by hour" : "Sales by day"} />}
                  >
                    <table class="table">
                      <thead>
                        <tr>
                          <th scope="col">{byHour() ? "Hour" : "Day"}</th>
                          <th scope="col" class="n">Orders</th>
                          <th scope="col" class="n">Sales</th>
                        </tr>
                      </thead>
                      <tbody>
                        <For each={columns()}>
                          {(c) => (
                            <tr>
                              <th scope="row">{c.label}</th>
                              <td class="n">{c.detail.replace(/ orders?$/, "")}</td>
                              <td class="n">{formatAmount(c.value)}</td>
                            </tr>
                          )}
                        </For>
                      </tbody>
                    </table>
                  </Show>
                  <button type="button" class="btn btn-plain btn-small" onClick={() => setAsTable(!asTable())}>
                    {asTable() ? "Show as a chart" : "Show as a table"}
                  </button>
                </Show>
              </section>

              <section class="panel">
                <div class="panel-head">
                  <h2>Cash counts</h2>
                  <Show when={shortCount() > 0}>
                    <span class="short">{shortCount() === 1 ? "1 count short" : `${shortCount()} counts short`}</span>
                  </Show>
                </div>
                <p class="meta">
                  What each till's drawer should hold at closing: its float, plus cash sales, less cash refunds.
                </p>
                <Show when={r().closes.length} fallback={<p class="empty">No till opened a day on these dates.</p>}>
                  <Show when={closes().length} fallback={<p class="empty">Every count came out exact.</p>}>
                    <div class="table-wrap">
                      <table class="table">
                        <thead>
                          <tr>
                            <th scope="col">Day</th>
                            <th scope="col">Till</th>
                            <th scope="col">Open</th>
                            <th scope="col" class="n">Float</th>
                            <th scope="col" class="n">Expected</th>
                            <th scope="col" class="n">Counted</th>
                            <th scope="col" class="n">Difference</th>
                          </tr>
                        </thead>
                        <tbody>
                          <For each={closes()}>{(c) => <CloseRow close={c} />}</For>
                        </tbody>
                      </table>
                    </div>
                  </Show>
                  <Show when={exactHidden() > 0 || allCloses()}>
                    <div class="row">
                      <Show when={!allCloses()}>
                        <span class="meta">
                          {exactHidden() === 1 ? "1 more count came out exact." : `${exactHidden()} more counts came out exact.`}
                        </span>
                      </Show>
                      <button type="button" class="btn btn-plain btn-small" onClick={() => setAllCloses(!allCloses())}>
                        {allCloses() ? "Show only the ones to check" : "Show all"}
                      </button>
                    </div>
                  </Show>
                </Show>
              </section>

              <section class="panel">
                <h2>What sold</h2>
                <Show when={r().items.length} fallback={<p class="empty">Nothing sold on these dates.</p>}>
                  <div class="table-wrap">
                    <table class="table items">
                      <thead>
                        <tr>
                          <th scope="col">Item</th>
                          <th scope="col" class="bar-cell">
                            <span class="visually-hidden">Share</span>
                          </th>
                          <th scope="col" class="n">Sold</th>
                          <th scope="col" class="n">Sales</th>
                          <th scope="col" class="n">Profit</th>
                          <th scope="col" class="n">Margin</th>
                        </tr>
                      </thead>
                      <tbody>
                        <For each={items()}>
                          {(it) => (
                            <tr>
                              <th scope="row">{it.name}</th>
                              <td class="bar-cell" aria-hidden="true">
                                <span class="meter" style={{ width: `${(it.grossFils / topItem()) * 100}%` }} />
                              </td>
                              <td class="n">{it.qty}</td>
                              <td class="n">{formatAmount(it.grossFils)}</td>
                              <Show when={it.costFils != null} fallback={<td class="n muted" colspan="2">No cost</td>}>
                                <td class="n">{formatAmount(it.grossFils - it.costFils!)}</td>
                                <td class="n">{margin(it.grossFils - it.costFils!, it.grossFils)}</td>
                              </Show>
                            </tr>
                          )}
                        </For>
                      </tbody>
                    </table>
                  </div>
                  <Show when={r().items.length > 10}>
                    <button type="button" class="btn btn-plain btn-small" onClick={() => setAllItems(!allItems())}>
                      {allItems() ? "Show the top 10" : `Show all ${r().items.length}`}
                    </button>
                  </Show>
                  <p class="meta">
                    Before refunds. Voided orders aren't counted. Profit is sales less what they cost when sold: set an
                    item's cost on <A href="/menu">Menu</A>.
                  </p>
                </Show>
              </section>

              <Show when={r().tills.length || r().staff.length}>
                <div class="pair">
                  <section class="panel">
                    <h2>By till</h2>
                    <table class="table">
                      <thead>
                        <tr>
                          <th scope="col">Till</th>
                          <th scope="col" class="n">Orders</th>
                          <th scope="col" class="n">Net</th>
                        </tr>
                      </thead>
                      <tbody>
                        <For each={r().tills}>
                          {(t) => (
                            <tr>
                              <th scope="row">
                                {t.code}
                                <Show when={t.name}>
                                  <span class="muted">, {t.name}</span>
                                </Show>
                              </th>
                              <td class="n">{t.orders}</td>
                              <td class="n">{formatAmount(t.netFils)}</td>
                            </tr>
                          )}
                        </For>
                      </tbody>
                    </table>
                  </section>
                  <section class="panel">
                    <h2>By staff</h2>
                    <table class="table">
                      <thead>
                        <tr>
                          <th scope="col">Who took the order</th>
                          <th scope="col" class="n">Orders</th>
                          <th scope="col" class="n">Sales</th>
                        </tr>
                      </thead>
                      <tbody>
                        <For each={r().staff}>
                          {(s) => (
                            <tr>
                              <th scope="row">{s.name}</th>
                              <td class="n">{s.orders}</td>
                              <td class="n">{formatAmount(s.grossFils)}</td>
                            </tr>
                          )}
                        </For>
                      </tbody>
                    </table>
                  </section>
                </div>
              </Show>
            </div>
          )}
        </Show>
        <Show when={!report() && !error()}>
          <p class="loading">Adding up the sales.</p>
        </Show>
      </section>

      <aside class="slip-wrap" aria-label="Totals">
        <Show when={report()}>
          {(r) => (
            <div class="slip" classList={{ stale: loading() }}>
              <p class="slip-date">
                {rangeLabel(r().from, r().to, today())}
                <Show when={shops().length > 1}>
                  <br />
                  {shop()?.name ?? "All shops"}
                </Show>
              </p>
              <p class="slip-label">Net sales</p>
              <p class="hero">
                <span class="cur">AED</span> {formatAmount(r().totals.netFils)}
              </p>
              <p class="meta">
                {orders(r().totals.orders)}
                <Show when={r().totals.orders > 0}>
                  , {formatAmount(Math.round(r().totals.grossFils / r().totals.orders))} on average
                </Show>
              </p>
              <dl class="slip-lines">
                <div>
                  <dt>Sales</dt>
                  <dd>{formatAmount(r().totals.grossFils)}</dd>
                </div>
                <div>
                  <dt>
                    Refunds
                    <Show when={r().totals.refunds > 0}> ({r().totals.refunds})</Show>
                  </dt>
                  <dd>{r().totals.refundsFils ? "-" + formatAmount(r().totals.refundsFils) : formatAmount(0)}</dd>
                </div>
              </dl>
              <Show
                when={r().totals.costFils > 0}
                fallback={
                  <Show when={r().totals.orders > 0}>
                    <p class="meta slip-note">
                      Give items their cost on <A href="/menu">Menu</A> to see the profit here.
                    </p>
                  </Show>
                }
              >
                <dl class="slip-lines">
                  <div>
                    <dt>What it cost</dt>
                    <dd>-{formatAmount(r().totals.costFils)}</dd>
                  </div>
                  <div class="profit">
                    <dt>Profit</dt>
                    <dd>{formatAmount(r().totals.profitFils)}</dd>
                  </div>
                </dl>
                <p class="meta slip-note">
                  A {margin(r().totals.profitFils, r().totals.profitFils + r().totals.costFils)} margin, after refunds.
                  <Show when={r().totals.uncostedFils > 0}>
                    {" "}It leaves out {formatAmount(r().totals.uncostedFils)} of sales of items without a cost.
                  </Show>
                </p>
              </Show>
              <dl class="slip-lines">
                <div>
                  <dt>Paid in cash</dt>
                  <dd>{formatAmount(r().totals.cashFils)}</dd>
                </div>
                <div>
                  <dt>Paid by card</dt>
                  <dd>{formatAmount(r().totals.cardFils)}</dd>
                </div>
                <Show when={r().totals.refundsCashFils > 0}>
                  <div>
                    <dt>Refunded in cash</dt>
                    <dd>-{formatAmount(r().totals.refundsCashFils)}</dd>
                  </div>
                </Show>
              </dl>
              <Show when={r().totals.voids > 0}>
                <dl class="slip-lines">
                  <div class="muted">
                    <dt>Voided, not counted ({r().totals.voids})</dt>
                    <dd>{formatAmount(r().totals.voidsFils)}</dd>
                  </div>
                </dl>
              </Show>
            </div>
          )}
        </Show>
        <For each={behind()}>
          {(t) => (
            <p class="meta behind">
              {t.code}
              {t.name ? `, ${t.name},` : ""}{" "}
              {t.lastSeenAt ? `was last in touch ${when(t.lastSeenAt)}.` : "hasn't been in touch yet."} Sales it made since
              will show once it is online.
            </p>
          )}
        </For>
      </aside>
    </div>
  );
}

/** "Ravi, 06:10 to 23:40", naming whoever closed when someone else did. */
function shift(c: DayClose): string {
  const opened = time(c.openedAt);
  if (!c.closedAt) return `${c.openedByName}, open since ${opened}`;
  const closed = time(c.closedAt);
  if (!c.closedByName || c.closedByName === c.openedByName) return `${c.openedByName}, ${opened} to ${closed}`;
  return `${c.openedByName} ${opened}, ${c.closedByName} ${closed}`;
}

function CloseRow(props: { close: DayClose }) {
  const c = () => props.close;
  const diff = () => c().differenceFils;
  return (
    <tr>
      <th scope="row">{shortDate(c().date, true)}</th>
      <td>{c().terminalCode}</td>
      <td class="who">{shift(c())}</td>
      <td class="n">{formatAmount(c().floatFils)}</td>
      <td class="n">{formatAmount(c().expectedFils)}</td>
      <td class="n">{c().countedFils !== undefined ? formatAmount(c().countedFils!) : ""}</td>
      <td class="n">
        <Show when={diff() !== undefined} fallback={<span class="muted">Not counted yet</span>}>
          <Show when={diff() !== 0} fallback={<span class="muted">Exact</span>}>
            <span classList={{ short: diff()! < 0 }}>
              {formatAmount(Math.abs(diff()!))} {diff()! < 0 ? "short" : "over"}
            </span>
          </Show>
        </Show>
      </td>
    </tr>
  );
}
