import { useSearchParams } from "@solidjs/router";
import { createEffect, createMemo, createSignal, For, Index, on, onMount, Show } from "solid-js";
import { createStore, produce } from "solid-js/store";
import {
  api,
  call,
  type Area,
  type Ingredient,
  type Product,
  type Purchase,
  type PurchaseInput,
  type PurchaseList,
  type Supplier,
  type Unit,
} from "../api/client";
import { areas, unitOf, units } from "../areas";
import { OverdueInvoices } from "../components/Overdue";
import { guessIngredient, guessIngredientName, guessPack, packFor, parsePack } from "../guess";
import { addDays, isDate, purchasePresets, rangeLabel, shortDate, todayIn } from "../dates";
import { formatAmount, formatFils, formatQuantity, parseAmount, parseQuantity } from "../money";
import { me } from "../state";
import { DeleteButton, EditorHead, fieldError, FormError, toSaveError, type SaveError } from "../ui";

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
  /** The supplier's code for the product, as on the invoice. */
  code: string;
  product: string;
  area: Area;
  qty: string;
  price: string;
  amount: string;
  /** Typed by hand, so quantity and price no longer set it. */
  amountTyped: boolean;
  /** For a new product: the ingredient it is ("" for none or later, or NEW), and how much one holds. */
  sortTo: string;
  pack: string;
  newName: string;
  newUnit: Unit;
  /** Chosen by hand, so the product's name no longer suggests them. */
  sortTouched: boolean;
};
const NEW = "new";
const unsorted = { sortTo: "", pack: "", newName: "", newUnit: "g" as Unit, sortTouched: false };
type Draft = {
  date: string;
  shopId: string;
  supplier: string;
  reference: string;
  lines: LineDraft[];
  check: string;
  /** Prices are before VAT, as on a wholesale invoice; amounts include it. */
  beforeVat: boolean;
  /** Start warning when the supplier's invoice is overdue. */
  watch: boolean;
};
/** The UAE's VAT, added to prices before VAT. */
const VAT = 0.05;

const blankLine = (): LineDraft => ({ code: "", product: "", area: "counter", qty: "", price: "", amount: "", amountTyped: false,
  ...unsorted });

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
  const [ingredients, setIngredients] = createSignal<Ingredient[]>([]);
  const loadNames = async () => {
    try {
      const [p, s, i] = await Promise.all([call(api.GET("/v1/admin/products")), call(api.GET("/v1/admin/suppliers")),
        call(api.GET("/v1/admin/ingredients"))]);
      setProducts(p);
      setSuppliers(s);
      setIngredients(i);
    } catch {
      // Only for suggestions; typing still works.
    }
  };
  onMount(loadNames);
  const productNamed = (name: string) => products().find((p) => p.name.toLowerCase() === name.trim().toLowerCase());
  const supplierNamed = (name: string) => suppliers().find((s) => s.name.toLowerCase() === name.trim().toLowerCase());

  // The editor.
  const [editing, setEditing] = createSignal<string | null | undefined>(undefined); // null: new
  const [d, setD] = createStore<Draft>({ date: "", shopId: "", supplier: "", reference: "", lines: [], check: "", beforeVat: false, watch: false });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  /** Counts saves and deletes, so the overdue warning looks again. */
  const [saves, setSaves] = createSignal(0);
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
            beforeVat: p.pricesBeforeVat,
            watch: false,
            lines: p.lines.map((l) => ({
              productId: l.productId,
              code: l.productCode ?? "",
              product: l.productName,
              area: l.area,
              qty: formatQuantity(l.quantity),
              price: formatFils(l.unitPriceFils),
              amount: formatFils(l.totalFils),
              amountTyped: l.totalFils !== Math.round(l.quantity * l.unitPriceFils * (p.pricesBeforeVat ? 1 + VAT : 1)),
              ...unsorted,
            })),
          }
        : { date: today(), shopId: shop()?.id ?? shops()[0]?.id ?? "", supplier: "", reference: "", check: "",
            beforeVat: false, watch: false, lines: [blankLine()] },
    );
    setEditing(p?.id ?? null);
  };

  // Quantity times price, with VAT when the prices are before it, unless
  // the amount was typed.
  const reprice = (i: number) => {
    const l = d.lines[i];
    if (!l || l.amountTyped) return;
    const q = parseQuantity(l.qty);
    const p = money(l.price);
    setD("lines", i, "amount", q !== null && p !== null ? formatFils(Math.round(q * p * (d.beforeVat ? 1 + VAT : 1))) : "");
  };
  const setBeforeVat = (on: boolean) => {
    setD("beforeVat", on);
    d.lines.forEach((_, i) => reprice(i));
  };
  /** The VAT in a line's amount: what it is over quantity times price. */
  const lineVat = (l: LineDraft) => {
    if (!d.beforeVat) return 0;
    const q = parseQuantity(l.qty);
    const p = money(l.price);
    const amount = money(l.amount);
    if (q === null || p === null || amount === null) return 0;
    return Math.max(0, amount - Math.round(q * p));
  };
  const productCoded = (code: string) => {
    const c = code.trim();
    return c ? products().find((p) => p.code === c) : undefined;
  };
  // A known code fills in its product.
  const setCode = (i: number, code: string) => {
    const known = productCoded(code);
    setD("lines", i, "code", code);
    if (known) setProduct(i, known.name);
  };
  const setProduct = (i: number, name: string) => {
    const known = productNamed(name);
    setD(produce((x) => {
      const l = x.lines[i]!;
      l.product = name;
      l.productId = known?.id;
      if (known) {
        l.area = known.area;
        if (!l.code && known.code) l.code = known.code;
        if (!l.price && known.lastUnitPriceFils != null) l.price = formatFils(known.lastUnitPriceFils);
      } else if (!l.sortTouched) {
        // What the name suggests, for a person to check.
        const ing = guessIngredient(name, ingredients());
        const size = guessPack(name);
        l.sortTo = ing?.id ?? "";
        l.pack = String(packFor(name, ing?.unit) ?? "");
        const sibling = ing && products().find((p) => p.ingredientId === ing.id);
        if (sibling) l.area = sibling.area;
        l.newName = guessIngredientName(name);
        l.newUnit = size?.unit ?? "g";
      }
    }));
    reprice(i);
  };
  const isNew = (l: LineDraft) => l.product.trim() !== "" && !productNamed(l.product);
  const lineUnit = (l: LineDraft): Unit | undefined =>
    l.sortTo === NEW ? l.newUnit : ingredients().find((x) => x.id === l.sortTo)?.unit;
  const setSortTo = (i: number, to: string) =>
    setD(produce((x) => {
      const l = x.lines[i]!;
      l.sortTo = to;
      l.sortTouched = true;
      const unit = to === NEW ? l.newUnit : ingredients().find((y) => y.id === to)?.unit;
      l.pack = String(packFor(l.product, unit) ?? "");
      // The area its ingredient's other products go to.
      const sibling = products().find((p) => p.ingredientId === to);
      if (sibling) l.area = sibling.area;
    }));
  const packHint = (l: LineDraft) => {
    const unit = lineUnit(l);
    if (!unit) return "";
    if (!l.pack.trim()) return `How much one holds, in ${unitOf(unit).label.toLowerCase()}, as 48 x 170. Or leave it for later.`;
    const n = parsePack(l.pack);
    return n === null ? "Type a number, or one like 48 x 170." : `= ${new Intl.NumberFormat("en").format(n)} ${unitOf(unit).short}`;
  };

  const lineTotal = (l: LineDraft) => money(l.amount) ?? 0;
  const total = createMemo(() => d.lines.reduce((s, l) => s + lineTotal(l), 0));
  const totalVat = createMemo(() => d.lines.reduce((s, l) => s + lineVat(l), 0));
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
          const last = r.purchases.find((p) => p.supplier?.id === sid);
          setLastOf(last);
          // A supplier's invoices are alike: before VAT if its last was.
          if (last && blankDraft()) setBeforeVat(last.pricesBeforeVat);
        } catch {
          // Only a shortcut.
        }
      },
    ),
  );
  const copyLast = () => {
    const p = lastOf();
    if (!p) return;
    setD("beforeVat", p.pricesBeforeVat);
    setD("lines", p.lines.map((l) => ({
      productId: l.productId, code: l.productCode ?? "", product: l.productName, area: l.area, qty: "",
      price: formatFils(l.unitPriceFils), amount: "", amountTyped: false, ...unsorted,
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
      // New ingredients first, one per name, so lines can name them.
      const made = new Map<string, string>();
      for (const [i, l] of kept.entries()) {
        if (!isNew(l) || l.sortTo !== NEW) continue;
        const name = l.newName.trim();
        if (!name) throw { field: `lines.${i}.newName`, message: "Name the new ingredient, as Basmati rice." };
        const key = name.toLowerCase();
        if (made.has(key)) continue;
        const known = ingredients().find((x) => x.name.toLowerCase() === key);
        if (known) {
          made.set(key, known.id);
          continue;
        }
        try {
          const x = await call(api.POST("/v1/admin/ingredients", { body: { name, unit: l.newUnit } }));
          made.set(key, x.id);
          // Known from now on, so a retry after a refused save reuses it.
          setIngredients((list) => [...list, x]);
        } catch (e) {
          throw { field: `lines.${i}.newName`, message: (e as Error).message };
        }
      }
      const lines = kept.map((l, i) => {
          const f = `lines.${i}`;
          const quantity = parseQuantity(l.qty);
          if (quantity === null) throw { field: `${f}.quantity`, message: "Enter how many, like 2 or 1.5." };
          const unitPriceFils = l.price.trim() ? money(l.price) : 0;
          if (unitPriceFils === null) throw { field: `${f}.unitPriceFils`, message: "Enter a price like 105.00." };
          const totalFils = money(l.amount);
          if (totalFils === null) throw { field: `${f}.totalFils`, message: "Enter the amount, like 210.00." };
          const known = l.productId && productNamed(l.product)?.id === l.productId ? l.productId : undefined;
          let ingredientId: string | undefined;
          let packAmount: number | undefined;
          if (isNew(l) && l.sortTo) {
            ingredientId = l.sortTo === NEW ? made.get(l.newName.trim().toLowerCase()) : l.sortTo;
            if (l.pack.trim()) {
              const n = parsePack(l.pack);
              if (n === null) throw { field: `${f}.packAmount`, message: "Type how much one holds, as 10000 or 48 x 170." };
              packAmount = n;
            }
          }
          return { productId: known, productName: known ? undefined : l.product, productCode: l.code.trim() || undefined,
            area: l.area, quantity, unitPriceFils, totalFils, vatFils: d.beforeVat ? lineVat(l) : undefined,
            ingredientId, packAmount };
        });
      const supplier = supplierNamed(d.supplier);
      const body: PurchaseInput = {
        shopId: d.shopId,
        date: d.date,
        supplierId: supplier?.id,
        supplierName: supplier ? undefined : d.supplier.trim() || undefined,
        reference: d.reference,
        pricesBeforeVat: d.beforeVat,
        lines,
      };
      const id = editing();
      const saved = id
        ? await call(api.PUT("/v1/admin/purchases/{id}", { params: { path: { id } }, body }))
        : await call(api.POST("/v1/admin/purchases", { body }));
      if (supplier && !supplier.watched && d.watch) {
        await call(api.PUT("/v1/admin/suppliers/{id}", { params: { path: { id: supplier.id } },
          body: { watched: true, quietUntil: null } }));
      }
      await Promise.all([load(), loadNames()]);
      setSaves((n) => n + 1);
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
      setSaves((n) => n + 1);
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
        <OverdueInvoices refresh={saves()} onAdd={(name) => {
          open(null);
          setD("supplier", name);
        }} />
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
          <Show when={supplierNamed(d.supplier) && !supplierNamed(d.supplier)!.watched}>
            <label class="check">
              <input type="checkbox" checked={d.watch} onChange={(e) => setD("watch", e.currentTarget.checked)} />
              <span>Warn me when an invoice from {supplierNamed(d.supplier)!.name} is overdue</span>
            </label>
          </Show>
          <label class="check">
            <input type="checkbox" checked={d.beforeVat} onChange={(e) => setBeforeVat(e.currentTarget.checked)} />
            <span>Prices are before 5% VAT, as on a wholesale invoice</span>
          </label>
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
                  <div class="line-head">
                    <label class="field">
                      <span class="visually-hidden">Item code</span>
                      <input class="input" list="codes" placeholder="Code" value={l().code}
                        onInput={(e) => setCode(i, e.currentTarget.value)}
                        aria-invalid={fieldError(error(), `lines.${i}.productCode`) ? "true" : undefined} />
                    </label>
                    <label class="field">
                      <span class="visually-hidden">Product</span>
                      <input class="input" list="products" placeholder="Product, as on the invoice" value={l().product}
                        onInput={(e) => setProduct(i, e.currentTarget.value)}
                        aria-invalid={fieldError(error(), `lines.${i}.productName`) ? "true" : undefined} />
                    </label>
                  </div>
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
                      <span>{d.beforeVat ? "Rate, before VAT" : "Price each"}</span>
                      <input class="input" inputmode="decimal" value={l().price}
                        onInput={(e) => { setD("lines", i, "price", e.currentTarget.value); reprice(i); }}
                        aria-invalid={fieldError(error(), `lines.${i}.unitPriceFils`) ? "true" : undefined} />
                    </label>
                    <label class="field">
                      <span>{d.beforeVat ? "With VAT" : "Amount"}</span>
                      <input class="input" inputmode="decimal" value={l().amount}
                        onInput={(e) => {
                          const v = e.currentTarget.value;
                          setD("lines", i, (x) => ({ ...x, amount: v, amountTyped: v.trim() !== "" }));
                          if (!v.trim()) reprice(i);
                        }}
                        aria-invalid={fieldError(error(), `lines.${i}.totalFils`) ? "true" : undefined} />
                    </label>
                  </div>
                  <Show when={isNew(l())}>
                    <div class="sort-row">
                      <label class="field">
                        <span>New product. Which ingredient is it?</span>
                        <select class="input" value={l().sortTo} onChange={(e) => setSortTo(i, e.currentTarget.value)}>
                          <option value="">None, or sort it later</option>
                          <For each={ingredients()}>{(x) => <option value={x.id}>{x.name}</option>}</For>
                          <option value={NEW}>A new ingredient…</option>
                        </select>
                      </label>
                      <Show when={l().sortTo === NEW}>
                        <div class="mod-row">
                          <label class="field">
                            <span>Its name</span>
                            <input class="input" value={l().newName}
                              onInput={(e) => setD("lines", i, "newName", e.currentTarget.value)}
                              aria-invalid={fieldError(error(), `lines.${i}.newName`) ? "true" : undefined} />
                          </label>
                          <label class="field">
                            <span>Measured in</span>
                            <select class="input" value={l().newUnit} onChange={(e) => {
                              setD("lines", i, "newUnit", e.currentTarget.value as Unit);
                              setSortTo(i, NEW);
                            }}>
                              <For each={units}>{(u) => <option value={u.id}>{u.label}</option>}</For>
                            </select>
                          </label>
                        </div>
                      </Show>
                      <Show when={lineUnit(l())}>
                        <label class="field">
                          <span>One holds ({unitOf(lineUnit(l())!).short})</span>
                          <input class="input" inputmode="decimal" value={l().pack}
                            onInput={(e) => setD("lines", i, "pack", e.currentTarget.value)}
                            aria-invalid={fieldError(error(), `lines.${i}.packAmount`) ? "true" : undefined} />
                          <span class="meta">{packHint(l())}</span>
                        </label>
                      </Show>
                    </div>
                  </Show>
                  <For each={["productCode", "quantity", "unitPriceFils", "totalFils", "vatFils", "newName", "packAmount"]}>
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
            <datalist id="codes">
              <For each={products().filter((p) => p.code)}>{(p) => <option value={p.code!}>{p.name}</option>}</For>
            </datalist>
            <button type="button" class="btn btn-option" onClick={() => setD("lines", (ls) => [...ls, blankLine()])}>
              Add a line
            </button>
          </div>

          <div class="invoice-foot">
            <div class="invoice-total">
              <span>Total{d.beforeVat ? ", with VAT" : ""}</span>
              <strong class="num">{formatAmount(total())}</strong>
            </div>
            <Show when={d.beforeVat}>
              <p class="meta vat-line">Of which VAT, {formatAmount(totalVat())}</p>
            </Show>
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
