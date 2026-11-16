import { useSearchParams } from "@solidjs/router";
import { createEffect, createMemo, createSignal, For, Index, on, onMount, Show } from "solid-js";
import { createStore, produce } from "solid-js/store";
import {
  api,
  call,
  type Area,
  type Product,
  type Purchase,
  type PurchaseInput,
  type PurchaseList,
  type Supplier,
} from "../api/client";
import { addDays, isDate, purchasePresets, rangeLabel, shortDate, todayIn } from "../dates";
import { formatAmount, formatFils, formatQuantity, parseAmount, parseQuantity } from "../money";
import { me } from "../state";
import { DeleteButton, EditorHead, fieldError, FormError, toSaveError, type SaveError } from "../ui";

const areas: { id: Area; label: string; sales?: string }[] = [
  { id: "counter", label: "Counter", sales: "counter sales" },
  { id: "kitchen", label: "Kitchen", sales: "kitchen sales" },
  { id: "packaging", label: "Packaging" },
  { id: "other", label: "Other" },
];
const share = (part: number, whole: number) => {
  if (whole <= 0) return "";
  const pct = Math.round((part / whole) * 100);
  return pct === 0 && part > 0 ? "under 1%" : `${pct}%`;
};
/** An amount as typed, allowing thousands commas: "2,840.00". */
const money = (s: string) => parseAmount(s.replace(/,/g, ""));
const when = (iso: string) =>
  new Date(iso).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });

type LineDraft = {
  productId?: string;
  product: string;
  area: Area;
  qty: string;
  price: string;
  amount: string;
  /** Typed by hand, so quantity and price no longer set it. */
  amountTyped: boolean;
};
type Draft = { date: string; shopId: string; supplier: string; reference: string; lines: LineDraft[]; check: string };

const blankLine = (): LineDraft => ({ product: "", area: "counter", qty: "", price: "", amount: "", amountTyped: false });

/** Purchases: what was bought, and where the money went. */
export function PurchasesPage() {
  const [params, setParams] = useSearchParams<{ from?: string; to?: string; shop?: string }>();
  const shops = () => me()?.shops ?? [];
  const shop = () => shops().find((s) => s.id === params.shop);
  const timeZone = () => (shop() ?? shops()[0])?.timezone ?? "Asia/Dubai";
  const today = () => todayIn(timeZone());
  const defaultRange = () => purchasePresets.find((p) => p.id === "month")!.range(today());
  const from = () => (isDate(params.from) ? params.from : defaultRange()[0]);
  const to = () => (isDate(params.to) && params.to >= from() ? params.to : defaultRange()[1]);
  const preset = () =>
    purchasePresets.find((p) => {
      const [f, t] = p.range(today());
      return f === from() && t === to();
    });

  const [list, setList] = createSignal<PurchaseList>();
  const [loading, setLoading] = createSignal(false);
  const [loadError, setLoadError] = createSignal("");
  let asked = 0;
  const load = async () => {
    const mine = ++asked;
    setLoading(true);
    try {
      const query = { from: from(), to: to(), shopId: shop() ? params.shop : undefined };
      const r = await call(api.GET("/v1/admin/purchases", { params: { query } }));
      if (mine === asked) {
        setList(r);
        setLoadError("");
      }
    } catch (e) {
      if (mine === asked) setLoadError((e as Error).message);
    } finally {
      if (mine === asked) setLoading(false);
    }
  };
  createEffect(on([from, to, () => params.shop], load));

  const [products, setProducts] = createSignal<Product[]>([]);
  const [suppliers, setSuppliers] = createSignal<Supplier[]>([]);
  const loadNames = async () => {
    try {
      const [p, s] = await Promise.all([call(api.GET("/v1/admin/products")), call(api.GET("/v1/admin/suppliers"))]);
      setProducts(p);
      setSuppliers(s);
    } catch {
      // Only for suggestions; typing still works.
    }
  };
  onMount(loadNames);
  const productNamed = (name: string) => products().find((p) => p.name.toLowerCase() === name.trim().toLowerCase());
  const supplierNamed = (name: string) => suppliers().find((s) => s.name.toLowerCase() === name.trim().toLowerCase());

  // The editor.
  const [editing, setEditing] = createSignal<string | null | undefined>(undefined); // null: new
  const [d, setD] = createStore<Draft>({ date: "", shopId: "", supplier: "", reference: "", lines: [], check: "" });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const edited = () => list()?.purchases.find((p) => p.id === editing());

  const open = (p: Purchase | null) => {
    setError(undefined);
    setStatus("");
    setD(
      p
        ? {
            date: p.date,
            shopId: p.shopId,
            supplier: p.supplier?.name ?? "",
            reference: p.reference,
            check: "",
            lines: p.lines.map((l) => ({
              productId: l.productId,
              product: l.productName,
              area: l.area,
              qty: formatQuantity(l.quantity),
              price: formatFils(l.unitPriceFils),
              amount: formatFils(l.totalFils),
              amountTyped: l.totalFils !== Math.round(l.quantity * l.unitPriceFils),
            })),
          }
        : { date: today(), shopId: shop()?.id ?? shops()[0]?.id ?? "", supplier: "", reference: "", check: "",
            lines: [blankLine()] },
    );
    setEditing(p?.id ?? null);
  };

  // Quantity times price, unless the amount was typed.
  const reprice = (i: number) => {
    const l = d.lines[i];
    if (!l || l.amountTyped) return;
    const q = parseQuantity(l.qty);
    const p = money(l.price);
    setD("lines", i, "amount", q !== null && p !== null ? formatFils(Math.round(q * p)) : "");
  };
  const setProduct = (i: number, name: string) => {
    const known = productNamed(name);
    setD(produce((x) => {
      const l = x.lines[i]!;
      l.product = name;
      l.productId = known?.id;
      if (known) {
        l.area = known.area;
        if (!l.price && known.lastUnitPriceFils != null) l.price = formatFils(known.lastUnitPriceFils);
      }
    }));
    reprice(i);
  };

  const lineTotal = (l: LineDraft) => money(l.amount) ?? 0;
  const total = createMemo(() => d.lines.reduce((s, l) => s + lineTotal(l), 0));
  const check = () => {
    if (!d.check.trim()) return null;
    const c = money(d.check);
    if (c === null) return { ok: false, text: "Enter the invoice's total, like 2,840.00." };
    const diff = total() - c;
    if (diff === 0) return { ok: true, text: "The lines match the invoice." };
    return {
      ok: false,
      text: `The lines come to ${formatFils(Math.abs(diff))} ${diff > 0 ? "more" : "less"} than the invoice. Check them.`,
    };
  };

  // A supplier's last invoice, to start this one from.
  const [lastOf, setLastOf] = createSignal<Purchase>();
  createEffect(
    on(
      () => [editing(), supplierNamed(d.supplier)?.id] as const,
      async ([id, sid]) => {
        setLastOf(undefined);
        if (id !== null || !sid) return;
        try {
          const r = await call(api.GET("/v1/admin/purchases", { params: { query: { from: addDays(today(), -365), to: today() } } }));
          setLastOf(r.purchases.find((p) => p.supplier?.id === sid));
        } catch {
          // Only a shortcut.
        }
      },
    ),
  );
  const copyLast = () => {
    const p = lastOf();
    if (!p) return;
    setD("lines", p.lines.map((l) => ({
      productId: l.productId, product: l.productName, area: l.area, qty: "", price: formatFils(l.unitPriceFils),
      amount: "", amountTyped: false,
    })));
  };
  const blankDraft = () => d.lines.every((l) => !l.product && !l.qty && !l.amount);

  const save = async () => {
    setBusy(true);
    setError(undefined);
    setStatus("");
    try {
      // Empty lines go, so the rest keep the places errors name.
      const kept = d.lines.filter((l) => l.product.trim() || l.qty.trim() || l.amount.trim());
      if (kept.length !== d.lines.length) setD("lines", kept.length ? kept : [blankLine()]);
      const lines = kept.map((l, i) => {
          const f = `lines.${i}`;
          const quantity = parseQuantity(l.qty);
          if (quantity === null) throw { field: `${f}.quantity`, message: "Enter how many, like 2 or 1.5." };
          const unitPriceFils = l.price.trim() ? money(l.price) : 0;
          if (unitPriceFils === null) throw { field: `${f}.unitPriceFils`, message: "Enter a price like 105.00." };
          const totalFils = money(l.amount);
          if (totalFils === null) throw { field: `${f}.totalFils`, message: "Enter the amount, like 210.00." };
          const known = l.productId && productNamed(l.product)?.id === l.productId ? l.productId : undefined;
          return { productId: known, productName: known ? undefined : l.product, area: l.area, quantity, unitPriceFils, totalFils };
        });
      const supplier = supplierNamed(d.supplier);
      const body: PurchaseInput = {
        shopId: d.shopId,
        date: d.date,
        supplierId: supplier?.id,
        supplierName: supplier ? undefined : d.supplier.trim() || undefined,
        reference: d.reference,
        lines,
      };
      const id = editing();
      const saved = id
        ? await call(api.PUT("/v1/admin/purchases/{id}", { params: { path: { id } }, body }))
        : await call(api.POST("/v1/admin/purchases", { body }));
      await Promise.all([load(), loadNames()]);
      setEditing(saved.id);
      open(saved);
      setStatus(id ? "Saved." : "Purchase recorded.");
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };
  const remove = async (id: string) => {
    setBusy(true);
    try {
      await call(api.DELETE("/v1/admin/purchases/{id}", { params: { path: { id } } }));
      setEditing(undefined);
      await load();
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const pick = (range: [string, string]) => setParams({ from: range[0], to: range[1] });

  return (
    <div class="workspace purchases">
      <section class="wall" aria-label="Purchases">
        <div class="wall-head">
          <h1>Purchases</h1>
          <span class="spacer" />
          <button type="button" class="btn btn-ink" onClick={() => open(null)}>
            Add a purchase
          </button>
        </div>
        <div class="filters" role="group" aria-label="Dates">
          <For each={purchasePresets}>
            {(p) => (
              <button type="button" class="chip" aria-pressed={preset()?.id === p.id} onClick={() => pick(p.range(today()))}>
                {p.label}
              </button>
            )}
          </For>
          <span class="dates">
            <label>
              <span class="visually-hidden">From</span>
              <input type="date" class="input date" value={from()} max={today()}
                onChange={(e) => isDate(e.currentTarget.value) &&
                  setParams({ from: e.currentTarget.value, to: to() < e.currentTarget.value ? e.currentTarget.value : to() })} />
            </label>
            <span class="muted">to</span>
            <label>
              <span class="visually-hidden">To</span>
              <input type="date" class="input date" value={to()} max={today()}
                onChange={(e) => isDate(e.currentTarget.value) &&
                  setParams({ to: e.currentTarget.value, from: from() > e.currentTarget.value ? e.currentTarget.value : from() })} />
            </label>
          </span>
          <Show when={shops().length > 1}>
            <select class="input shop" value={params.shop ?? ""} onChange={(e) => setParams({ shop: e.currentTarget.value || undefined })}>
              <option value="">All shops</option>
              <For each={shops()}>{(s) => <option value={s.id}>{s.name}</option>}</For>
            </select>
          </Show>
        </div>
        <Show when={loadError()}>
          <p class="banner" role="alert">{loadError()}</p>
        </Show>

        <Show when={list()} fallback={<Show when={!loadError()}><p class="loading">Adding up the purchases.</p></Show>}>
          {(r) => (
            <div class="report" classList={{ stale: loading() }} aria-busy={loading()}>
              <section class="panel">
                <div class="panel-head">
                  <h2>Where the money went</h2>
                  <span class="meta">{rangeLabel(r().from, r().to, today())}</span>
                </div>
                <table class="table">
                  <thead>
                    <tr>
                      <th scope="col">Area</th>
                      <th scope="col" class="n">Bought</th>
                      <th scope="col">Against sales</th>
                    </tr>
                  </thead>
                  <tbody>
                    <For each={r().areas}>
                      {(a) => {
                        const info = areas.find((x) => x.id === a.area)!;
                        const sales = () => a.salesFils ?? r().salesFils;
                        return (
                          <tr>
                            <th scope="row">{info.label}</th>
                            <td class="n">{formatAmount(a.boughtFils)}</td>
                            <td class="against">
                              <Show when={a.boughtFils > 0 && sales() > 0} fallback={<span class="muted">{a.boughtFils ? "No sales" : ""}</span>}>
                                <strong>{share(a.boughtFils, sales())}</strong>
                                <span class="muted"> of {formatAmount(sales())} {info.sales ?? "sales in all"}</span>
                              </Show>
                            </td>
                          </tr>
                        );
                      }}
                    </For>
                    <tr class="all">
                      <th scope="row">All</th>
                      <td class="n">{formatAmount(r().totalFils)}</td>
                      <td class="against">
                        <Show when={r().totalFils > 0 && r().salesFils > 0}>
                          <strong>{share(r().totalFils, r().salesFils)}</strong>
                          <span class="muted"> of {formatAmount(r().salesFils)} sales</span>
                        </Show>
                      </td>
                    </tr>
                  </tbody>
                </table>
                <p class="meta">
                  Sales are less refunds. Kitchen sales are the items on the kitchen ticket; counter sales, the rest.
                  Stock bought in one week is used over the next, so a month says more than a week.
                </p>
              </section>

              <section class="panel">
                <h2>{r().purchases.length === 1 ? "1 purchase" : `${r().purchases.length} purchases`}</h2>
                <Show when={r().purchases.length} fallback={
                  <p class="empty">Nothing bought on these dates. Add a purchase from its invoice or receipt.</p>
                }>
                  <div class="list purchase-list">
                    <For each={r().purchases}>
                      {(p) => (
                        <button type="button" aria-pressed={editing() === p.id} onClick={() => open(p)}>
                          <span class="when">{shortDate(p.date, true)}</span>
                          <span class="what">
                            <strong>{p.supplier?.name ?? "One-off"}</strong>
                            <span class="meta">
                              {p.reference ? `${p.reference}, ` : ""}
                              {p.lines.length === 1 ? p.lines[0]!.productName : `${p.lines.length} lines`}
                            </span>
                          </span>
                          <span class="num amount">{formatAmount(p.totalFils)}</span>
                        </button>
                      )}
                    </For>
                  </div>
                </Show>
              </section>
            </div>
          )}
        </Show>
      </section>

      <aside class="editor invoice" classList={{ closed: editing() === undefined }} aria-label="Purchase">
        <Show when={editing() !== undefined} fallback={
          <p class="editor-empty">Choose a purchase to see or change it, or add one from its invoice or receipt.</p>
        }>
          <EditorHead title={editing() ? `Purchase of ${shortDate(d.date)}` : "New purchase"} onClose={() => setEditing(undefined)} />
          <FormError error={error()} fields={["date", "supplierName", "reference", "lines\\.\\d+\\.\\w+"]} />
          <div class="mod-row">
            <label class="field">
              <span>Date</span>
              <input type="date" class="input" value={d.date} max={today()} onChange={(e) => setD("date", e.currentTarget.value)}
                aria-invalid={fieldError(error(), "date") ? "true" : undefined} />
            </label>
            <label class="field">
              <span>Invoice no. (optional)</span>
              <input class="input" value={d.reference} onInput={(e) => setD("reference", e.currentTarget.value)} />
            </label>
          </div>
          <Show when={shops().length > 1}>
            <label class="field">
              <span>For the shop</span>
              <select class="input" value={d.shopId} onChange={(e) => setD("shopId", e.currentTarget.value)}>
                <For each={shops()}>{(s) => <option value={s.id}>{s.name}</option>}</For>
              </select>
            </label>
          </Show>
          <label class="field">
            <span>Supplier</span>
            <input class="input" list="suppliers" value={d.supplier} onInput={(e) => setD("supplier", e.currentTarget.value)}
              aria-invalid={fieldError(error(), "supplierName") ? "true" : undefined} />
            <span class="meta">
              {d.supplier.trim() && !supplierNamed(d.supplier)
                ? "A new supplier, added when you save."
                : "Empty for a one-off from anywhere."}
            </span>
          </label>
          <datalist id="suppliers">
            <For each={suppliers()}>{(s) => <option value={s.name} />}</For>
          </datalist>
          <Show when={lastOf() && blankDraft()}>
            <button type="button" class="btn btn-option copy" onClick={copyLast}>
              Copy the lines of the last invoice, {shortDate(lastOf()!.date)}
            </button>
          </Show>

          <div class="field">
            <span>Lines</span>
            <Index each={d.lines}>
              {(l, i) => (
                <div class="mod line">
                  <label class="field">
                    <span class="visually-hidden">Product</span>
                    <input class="input" list="products" placeholder="Product, as on the invoice" value={l().product}
                      onInput={(e) => setProduct(i, e.currentTarget.value)}
                      aria-invalid={fieldError(error(), `lines.${i}.productName`) ? "true" : undefined} />
                  </label>
                  <Show when={fieldError(error(), `lines.${i}.productName`)}>
                    <span class="error">{fieldError(error(), `lines.${i}.productName`)}</span>
                  </Show>
                  <div class="line-figures">
                    <label class="field">
                      <span>How many</span>
                      <input class="input" inputmode="decimal" value={l().qty}
                        onInput={(e) => { setD("lines", i, "qty", e.currentTarget.value); reprice(i); }}
                        aria-invalid={fieldError(error(), `lines.${i}.quantity`) ? "true" : undefined} />
                    </label>
                    <label class="field">
                      <span>Price each</span>
                      <input class="input" inputmode="decimal" value={l().price}
                        onInput={(e) => { setD("lines", i, "price", e.currentTarget.value); reprice(i); }}
                        aria-invalid={fieldError(error(), `lines.${i}.unitPriceFils`) ? "true" : undefined} />
                    </label>
                    <label class="field">
                      <span>Amount</span>
                      <input class="input" inputmode="decimal" value={l().amount}
                        onInput={(e) => {
                          const v = e.currentTarget.value;
                          setD("lines", i, (x) => ({ ...x, amount: v, amountTyped: v.trim() !== "" }));
                          if (!v.trim()) reprice(i);
                        }}
                        aria-invalid={fieldError(error(), `lines.${i}.totalFils`) ? "true" : undefined} />
                    </label>
                  </div>
                  <For each={["quantity", "unitPriceFils", "totalFils"]}>
                    {(f) => (
                      <Show when={fieldError(error(), `lines.${i}.${f}`)}>
                        <span class="error">{fieldError(error(), `lines.${i}.${f}`)}</span>
                      </Show>
                    )}
                  </For>
                  <div class="row">
                    <label class="area-pick">
                      <span class="meta">For</span>
                      <select class="input" value={l().area} onChange={(e) => setD("lines", i, "area", e.currentTarget.value as Area)}>
                        <For each={areas}>{(a) => <option value={a.id}>{a.label}</option>}</For>
                      </select>
                    </label>
                    <span class="spacer" />
                    <button type="button" class="btn btn-danger btn-small" disabled={d.lines.length <= 1}
                      onClick={() => setD("lines", (ls) => ls.filter((_, j) => j !== i))}>
                      Remove
                    </button>
                  </div>
                </div>
              )}
            </Index>
            <datalist id="products">
              <For each={products()}>{(p) => <option value={p.name} />}</For>
            </datalist>
            <button type="button" class="btn btn-option" onClick={() => setD("lines", (ls) => [...ls, blankLine()])}>
              Add a line
            </button>
          </div>

          <div class="invoice-foot">
            <div class="invoice-total">
              <span>Total</span>
              <strong class="num">{formatAmount(total())}</strong>
            </div>
            <label class="field">
              <span>The invoice's own total, to check (optional)</span>
              <input class="input" inputmode="decimal" value={d.check} onInput={(e) => setD("check", e.currentTarget.value)} />
            </label>
            <Show when={check()}>
              {(c) => <p class={c().ok ? "meta matches" : "error"} role="status">{c().text}</p>}
            </Show>
          </div>

          <div class="row">
            <button type="button" class="btn btn-ink" disabled={busy()} onClick={save}>
              {editing() ? "Save purchase" : "Record purchase"}
            </button>
            <span class="spacer" />
            <Show when={edited()}>
              {(p) => <DeleteButton label="Delete" confirm="Tap again to delete" disabled={busy()} onDelete={() => remove(p().id)} />}
            </Show>
          </div>
          <p class="status" role="status">{status()}</p>
          <Show when={edited()}>
            {(p) => (
              <p class="meta">
                Entered by {p().createdByName}, {when(p().createdAt)}.
                <Show when={p().updatedAt}> Changed by {p().updatedByName}, {when(p().updatedAt!)}.</Show>
              </p>
            )}
          </Show>
        </Show>
      </aside>
    </div>
  );
}
