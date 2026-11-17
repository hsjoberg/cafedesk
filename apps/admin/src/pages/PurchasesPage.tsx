import { A, useSearchParams } from "@solidjs/router";
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
  type Usage,
} from "../api/client";
import { areas, unitOf, units } from "../areas";
import { OverdueInvoices } from "../components/Overdue";
import { guessIngredient, guessIngredientName, packFor, parsePack, suggestUnit } from "../guess";
import { addDays, isDate, purchasePresets, rangeLabel, shortDate, todayIn } from "../dates";
import { formatAmount, formatFils, formatQuantity, parseAmount, parseQuantity } from "../money";
import { catalog, me, reloadCatalog } from "../state";
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
  /** The unit the quantity counts in, as the invoice prints it: EA, CAR, PAC. Optional. */
  uom: string;
  /** For a new product: the ingredient it is ("" for none or later, or NEW), and how much one holds. */
  sortTo: string;
  pack: string;
  newName: string;
  newUnit: Unit;
  /** Chosen by hand, so the product's name no longer suggests them. */
  sortTouched: boolean;
  /** Typed by hand, so the name and unit no longer suggest it. */
  packTouched: boolean;
  /** With sortTo SOLD: the menu item the product is sold as, as it is. */
  soldAs: string;
};
const NEW = "new";
/** Sold as it is: the product is a menu item, one to one, such as a can. */
const SOLD = "sold";
const unsorted = { sortTo: "", pack: "", newName: "", newUnit: "g" as Unit, sortTouched: false, packTouched: false, soldAs: "" };
/** Units invoices print, offered as the line's unit is typed. */
const commonUoms = ["EA", "CAR", "PAC", "BAG", "BOX", "CTN", "PCS", "KG", "BTL", "CAN", "TIN"];
const normalUom = (u: string) => u.trim().toUpperCase();
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
/** The VAT a supplier adds to prices before VAT, as a fraction: the business's setting, 5% in the UAE. */
const VAT = () => (me()?.organization.vatPercent ?? 0) / 100;
/** The VAT as people write it: "5%". */
const vatLabel = () => `${me()?.organization.vatPercent ?? 0}%`;

const blankLine = (): LineDraft => ({ code: "", product: "", area: "counter", qty: "", price: "", amount: "", amountTyped: false, uom: "",
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
  const [usage, setUsage] = createSignal<Usage>();
  const [loading, setLoading] = createSignal(false);
  const [loadError, setLoadError] = createSignal("");
  let asked = 0;
  const load = async () => {
    const mine = ++asked;
    setLoading(true);
    try {
      const query = { from: from(), to: to(), shopId: shop() ? params.shop : undefined };
      const [r, u] = await Promise.all([call(api.GET("/v1/admin/purchases", { params: { query } })),
        call(api.GET("/v1/admin/reports/usage", { params: { query } }))]);
      if (mine === asked) {
        setList(r);
        setUsage(u);
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
              amountTyped: l.totalFils !== Math.round(l.quantity * l.unitPriceFils * (p.pricesBeforeVat ? 1 + VAT() : 1)),
              uom: l.uom,
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
    setD("lines", i, "amount", q !== null && p !== null ? formatFils(Math.round(q * p * (d.beforeVat ? 1 + VAT() : 1))) : "");
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
        if (!l.uom && known.lastUom) l.uom = known.lastUom;
      } else if (!l.sortTouched) {
        // What the name suggests, for a person to check.
        const ing = guessIngredient(name, ingredients());
        l.sortTo = ing?.id ?? "";
        const sibling = ing && products().find((p) => p.ingredientId === ing.id);
        if (sibling) l.area = sibling.area;
        l.newName = guessIngredientName(name);
        l.newUnit = suggestUnit(name);
      }
    }));
    suggestPack(i);
    reprice(i);
  };
  /** The product a line names, if it is one already bought. */
  const knownOf = (l: LineDraft) => (l.productId && products().find((p) => p.id === l.productId)) || productNamed(l.product);
  /**
   * How much one of the line's unit holds of a sorted product, if it knows:
   * its pack for the unit, else its main one where that goes for the line.
   */
  const knownPack = (l: LineDraft): number | null => {
    const p = knownOf(l);
    if (!p?.ingredientId) return null;
    const u = normalUom(l.uom);
    const own = u ? p.packs.find((x) => x.uom === u) : undefined;
    if (own) return own.packAmount;
    if (p.packAmount != null && (!u || p.packUom === "" || p.packUom === u)) return p.packAmount;
    return null;
  };
  /**
   * A sorted product bought in a unit it hasn't been yet: the line asks what
   * one holds, once. A pack size given without a unit is asked about too, so
   * the product learns whose it is.
   */
  const needsPack = (l: LineDraft) => {
    const p = knownOf(l);
    const u = normalUom(l.uom);
    if (!p?.ingredientId || !u) return false;
    if (p.packs.some((x) => x.uom === u)) return false;
    return !(p.packAmount != null && p.packUom === u);
  };
  /** Fills in what one holds, as the name and unit suggest, unless typed. */
  const suggestPack = (i: number) =>
    setD(produce((x) => {
      const l = x.lines[i]!;
      if (l.packTouched) return;
      const p = knownOf(l);
      if (p && needsPack(l)) {
        const ing = ingredients().find((y) => y.id === p.ingredientId);
        l.pack = p.packUom === "" && p.packAmount != null ? String(p.packAmount) : String(packFor(p.name, ing?.unit, l.uom) ?? "");
        return;
      }
      l.pack = String(packFor(l.product, lineUnit(l), l.uom) ?? "");
    }));
  const isNew = (l: LineDraft) => l.product.trim() !== "" && !productNamed(l.product);
  const lineUnit = (l: LineDraft): Unit | undefined => {
    if (l.sortTo === NEW) return l.newUnit;
    if (l.sortTo === SOLD) return "piece";
    const known = knownOf(l);
    const id = isNew(l) ? l.sortTo : known?.ingredientId;
    return ingredients().find((x) => x.id === id)?.unit;
  };
  const setSortTo = (i: number, to: string) =>
    setD(produce((x) => {
      const l = x.lines[i]!;
      l.sortTo = to;
      l.sortTouched = true;
      const unit = to === NEW ? l.newUnit : to === SOLD ? "piece" : ingredients().find((y) => y.id === to)?.unit;
      if (!l.packTouched) l.pack = String(packFor(l.product, unit, l.uom) ?? "");
      // The area its ingredient's other products go to.
      const sibling = products().find((p) => p.ingredientId === to);
      if (sibling) l.area = sibling.area;
    }));
  /** What one of the line's unit cost, as paid: its amount over how many. */
  const costEach = (l: LineDraft) => {
    const q = parseQuantity(l.qty);
    const a = money(l.amount);
    return q !== null && q > 0 && a !== null ? a / q : null;
  };
  /**
   * "2,000 g, at 24.13 per kg": how much, and its price by the kg, litre or
   * piece. n is how much one of the line's unit holds; times is how many
   * of them the amount shown is.
   */
  const amountAndPrice = (n: number, unit: Unit, l: LineDraft, times = 1) => {
    const u = unitOf(unit);
    const total = round3(n * times);
    let text = `${new Intl.NumberFormat("en").format(total)} ${unit === "piece" && total === 1 ? "piece" : u.short}`;
    const each = costEach(l);
    if (each !== null && n > 0) text += `, at ${formatFils(Math.round((each / n) * u.scale))} ${u.per}`;
    return text;
  };
  const packHint = (l: LineDraft) => {
    const unit = lineUnit(l);
    if (!unit) return "";
    if (!l.pack.trim()) return `How much one holds, in ${unitOf(unit).label.toLowerCase()}, as 48 x 170. Or leave it for later.`;
    const n = parsePack(l.pack);
    return n === null ? "Type a number, or one like 48 x 170." : `= ${amountAndPrice(n, unit, l)}.`;
  };
  /** For a sorted product whose size is known: what the line comes to, to check it. */
  const knownHint = (l: LineDraft) => {
    const p = knownOf(l);
    const pack = knownPack(l);
    const unit = lineUnit(l);
    const q = parseQuantity(l.qty);
    if (!p || pack === null || !unit || needsPack(l)) return "";
    const ing = ingredients().find((x) => x.id === p.ingredientId);
    return `${ing?.name ?? "It"}: ${amountAndPrice(pack, unit, l, q ?? 1)}${q === null ? " each" : ""}.`;
  };
  const round3 = (n: number) => Math.round(n * 1000) / 1000;
  const uomsSeen = createMemo(() => [...new Set([...commonUoms, ...products().flatMap((p) =>
    [p.packUom, p.lastUom ?? "", ...p.packs.map((x) => x.uom)]).filter(Boolean)])]);
  /** Menu items to sell a product as, with whether they have a recipe. */
  const menuItems = () => catalog()?.items ?? [];
  onMount(() => {
    if (!catalog()) void reloadCatalog();
  });

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
      price: formatFils(l.unitPriceFils), amount: "", amountTyped: false, uom: l.uom, ...unsorted,
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
      // New ingredients first, one per name, so lines can name them. A
      // product sold as it is gets one named after its menu item.
      const made = new Map<string, string>();
      const soldName = (l: LineDraft) => menuItems().find((it) => it.id === l.soldAs)?.name ?? "";
      for (const [i, l] of kept.entries()) {
        if (!isNew(l) || (l.sortTo !== NEW && l.sortTo !== SOLD)) continue;
        if (l.sortTo === SOLD && !l.soldAs) throw { field: `lines.${i}.soldAs`, message: "Choose the menu item it is sold as." };
        const name = l.sortTo === SOLD ? soldName(l) : l.newName.trim();
        const unit: Unit = l.sortTo === SOLD ? "piece" : l.newUnit;
        if (!name) throw { field: `lines.${i}.newName`, message: "Name the new ingredient, as Rice or Milk." };
        const key = name.toLowerCase();
        if (made.has(key)) continue;
        const known = ingredients().find((x) => x.name.toLowerCase() === key);
        if (known) {
          made.set(key, known.id);
          continue;
        }
        try {
          const x = await call(api.POST("/v1/admin/ingredients", { body: { name, unit } }));
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
            ingredientId = l.sortTo === NEW ? made.get(l.newName.trim().toLowerCase())
              : l.sortTo === SOLD ? made.get(soldName(l).toLowerCase()) : l.sortTo;
          }
          if ((isNew(l) && l.sortTo) || needsPack(l)) {
            if (l.pack.trim()) {
              const n = parsePack(l.pack);
              if (n === null) throw { field: `${f}.packAmount`, message: "Type how much one holds, as 10000 or 48 x 170." };
              packAmount = n;
            }
          }
          return { productId: known, productName: known ? undefined : l.product, productCode: l.code.trim() || undefined,
            area: l.area, quantity, unitPriceFils, totalFils, vatFils: d.beforeVat ? lineVat(l) : undefined,
            ingredientId, packAmount, uom: normalUom(l.uom) || undefined };
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
      // Each product sold as it is: its menu item's recipe is one of it,
      // unless the item has a recipe already.
      const recipes: string[] = [];
      for (const l of kept) {
        if (!isNew(l) || l.sortTo !== SOLD) continue;
        const item = menuItems().find((it) => it.id === l.soldAs);
        const ing = made.get(soldName(l).toLowerCase());
        if (!item || !ing || item.costFromRecipe || recipes.includes(item.id)) continue;
        await call(api.PUT("/v1/admin/items/{id}/recipe", { params: { path: { id: item.id } },
          body: { lines: [{ ingredientId: ing, amount: 1 }] } }));
        recipes.push(item.id);
      }
      if (recipes.length) void reloadCatalog();
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
              <For each={shops()}>{(s) => <option value={s.id} selected={(params.shop ?? "") === s.id}>{s.name}</option>}</For>
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

              <Show when={usage()}>
                {(u) => <UsagePanel usage={u()} />}
              </Show>

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
                <For each={shops()}>{(s) => <option value={s.id} selected={d.shopId === s.id}>{s.name}</option>}</For>
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
          <Show when={VAT() > 0 || d.beforeVat}>
            <label class="check">
              <input type="checkbox" checked={d.beforeVat} onChange={(e) => setBeforeVat(e.currentTarget.checked)} />
              <span>Prices are before {vatLabel()} VAT, as on a wholesale invoice</span>
            </label>
          </Show>
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
                      <span>Qty</span>
                      <input class="input" inputmode="decimal" value={l().qty}
                        onInput={(e) => { setD("lines", i, "qty", e.currentTarget.value); reprice(i); }}
                        aria-invalid={fieldError(error(), `lines.${i}.quantity`) ? "true" : undefined} />
                    </label>
                    <label class="field uom">
                      <span>Unit</span>
                      <input class="input" list="uoms" placeholder="EA" value={l().uom} autocapitalize="characters"
                        onInput={(e) => { setD("lines", i, "uom", e.currentTarget.value); suggestPack(i); }}
                        aria-invalid={fieldError(error(), `lines.${i}.uom`) ? "true" : undefined} />
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
                          <For each={ingredients()}>{(x) => <option value={x.id} selected={l().sortTo === x.id}>{x.name}</option>}</For>
                          <option value={NEW} selected={l().sortTo === NEW}>A new ingredient…</option>
                          <option value={SOLD} selected={l().sortTo === SOLD}>Sold as it is: a menu item…</option>
                        </select>
                      </label>
                      <Show when={l().sortTo === SOLD}>
                        <label class="field">
                          <span>The menu item</span>
                          <select class="input" value={l().soldAs} onChange={(e) => setD("lines", i, "soldAs", e.currentTarget.value)}
                            aria-invalid={fieldError(error(), `lines.${i}.soldAs`) ? "true" : undefined}>
                            <option value="" selected={!l().soldAs}>Choose…</option>
                            <For each={menuItems()}>
                              {(it) => <option value={it.id} selected={l().soldAs === it.id}>{it.name}</option>}
                            </For>
                          </select>
                          <span class="meta">
                            {menuItems().find((it) => it.id === l().soldAs)?.costFromRecipe
                              ? "It has a recipe already, which stays. The new ingredient is named after it."
                              : "Saving makes an ingredient named after it, and its recipe one of it."}
                          </span>
                        </label>
                      </Show>
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
                              <For each={units}>{(u) => <option value={u.id} selected={l().newUnit === u.id}>{u.label}</option>}</For>
                            </select>
                          </label>
                        </div>
                      </Show>
                      <Show when={lineUnit(l())}>
                        <label class="field">
                          <span>{l().uom.trim() ? `One ${normalUom(l().uom)} holds` : "One holds"} ({unitOf(lineUnit(l())!).short})</span>
                          <input class="input" inputmode="decimal" value={l().pack}
                            onInput={(e) => setD("lines", i, (x) => ({ ...x, pack: e.currentTarget.value, packTouched: true }))}
                            aria-invalid={fieldError(error(), `lines.${i}.packAmount`) ? "true" : undefined} />
                          <span class="meta">{packHint(l())}</span>
                        </label>
                      </Show>
                    </div>
                  </Show>
                  <Show when={!isNew(l()) && needsPack(l())}>
                    <div class="sort-row">
                      <label class="field">
                        <span>New unit for it. One {normalUom(l().uom)} holds ({unitOf(lineUnit(l())!).short})</span>
                        <input class="input" inputmode="decimal" value={l().pack}
                          onInput={(e) => setD("lines", i, (x) => ({ ...x, pack: e.currentTarget.value, packTouched: true }))}
                          aria-invalid={fieldError(error(), `lines.${i}.packAmount`) ? "true" : undefined} />
                        <span class="meta">{packHint(l()) || "Asked once: the next invoice in it knows."}</span>
                      </label>
                    </div>
                  </Show>
                  <Show when={knownHint(l())}>
                    <span class="meta line-check">{knownHint(l())}</span>
                  </Show>
                  <For each={["productCode", "uom", "quantity", "unitPriceFils", "totalFils", "vatFils", "newName", "soldAs", "packAmount"]}>
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
                        <For each={areas}>{(a) => <option value={a.id} selected={l().area === a.id}>{a.label}</option>}</For>
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
            <datalist id="uoms">
              <For each={uomsSeen()}>{(u) => <option value={u} />}</For>
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

/** An amount in its unit, in kg or litres from 1,000 up: "8.16 kg", "120 g", "5 pieces". */
function amountIn(n: number, unit: Unit): string {
  const a = Math.abs(n);
  const f = (x: number, d: number) => new Intl.NumberFormat("en", { maximumFractionDigits: d }).format(x);
  if (unit === "g") return a >= 1000 ? `${f(a / 1000, 2)} kg` : `${f(a, 1)} g`;
  if (unit === "ml") return a >= 1000 ? `${f(a / 1000, 2)} L` : `${f(a, 1)} ml`;
  return `${f(a, 1)} ${a === 1 ? "piece" : "pieces"}`;
}

/**
 * Bought against used: each ingredient's purchases set against what sales
 * used by the recipes. The difference is stock, waste, or a recipe off; the
 * panel says so, and what it leaves out.
 */
function UsagePanel(props: { usage: Usage }) {
  const u = () => props.usage;
  return (
    <section class="panel">
      <h2>Bought against used</h2>
      <Show when={u().ingredients.length} fallback={
        <p class="empty">
          Nothing to set side by side yet. It needs purchases sorted into ingredients, and items with recipes, on{" "}
          <A href="/ingredients">Ingredients</A> and <A href="/recipes">Recipes</A>.
        </p>
      }>
        <p>
          Ingredients bought for <strong>{formatAmount(u().boughtFils)}</strong>; by the recipes, sales used{" "}
          <strong>{formatAmount(u().usedFils)}</strong> of them.
        </p>
        <Show when={u().unrecipedFils > 0}>
          <p class="left-out">
            Only items with a recipe count as used. Sales of {formatAmount(u().unrecipedFils)} were of items without one, so
            "more bought" reads high until they have recipes, on <A href="/recipes">Recipes</A>.
          </p>
        </Show>
        <Show when={u().unsortedFils > 0}>
          <p class="meta">
            Purchases of products not sorted into an ingredient, {formatAmount(u().unsortedFils)}, aren't in "bought".
          </p>
        </Show>
        <div class="table-wrap">
          <table class="table usage">
            <thead>
              <tr>
                <th scope="col">Ingredient</th>
                <th scope="col" class="n">Bought</th>
                <th scope="col" class="n">Sales used</th>
                <th scope="col" class="n">Difference</th>
                <th scope="col" class="n">Worth</th>
              </tr>
            </thead>
            <tbody>
              <For each={u().ingredients}>
                {(x) => {
                  const diff = () => x.bought - x.used;
                  return (
                    <tr>
                      <th scope="row">{x.name}</th>
                      <td class="n">{x.bought ? amountIn(x.bought, x.unit) : ""}</td>
                      <td class="n">{x.used ? amountIn(x.used, x.unit) : ""}</td>
                      <td class="n">
                        <Show when={Math.abs(diff()) > 0.0005} fallback={<span class="muted">Even</span>}>
                          {amountIn(diff(), x.unit)} more {diff() > 0 ? "bought" : "used"}
                        </Show>
                      </td>
                      <td class="n">
                        <Show when={x.unitCostFils != null} fallback={<span class="muted">No price</span>}>
                          {formatAmount(Math.round(Math.abs(diff()) * x.unitCostFils!))}
                        </Show>
                      </td>
                    </tr>
                  );
                }}
              </For>
            </tbody>
          </table>
        </div>
        <p class="meta">
          More bought than used: stock on the shelf, waste, or a recipe that asks for too little. More used than bought:
          stock from before these dates, or a recipe that asks for too much. Without stock counts, a month says more than a
          week. Used is by today's recipes; refunded items count as used, voided ones don't.
        </p>
      </Show>
    </section>
  );
}
