import { A } from "@solidjs/router";
import { createMemo, createSignal, For, onMount, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { api, call, type Area, type Ingredient, type Product, type Unit } from "../api/client";
import { areaLabel, areas, unitOf, units } from "../areas";
import { shortDate } from "../dates";
import { guessIngredientName, packFor, parsePack, suggestUnit } from "../guess";
import { formatAmount } from "../money";
import { catalog, reloadCatalog } from "../state";
import { DeleteButton, EditorHead, Field, fieldError, FormError, toSaveError, type SaveError } from "../ui";

const grouped = new Intl.NumberFormat("en", { maximumFractionDigits: 3 });

/** An amount with its unit: "8,160 g", "1 piece", "100 pieces". */
function amountOf(n: number, unit: Unit) {
  const short = unit === "piece" ? (n === 1 ? "piece" : "pieces") : unitOf(unit).short;
  return `${grouped.format(n)} ${short}`;
}

/** A price per unit in fils, as per kg, per litre or each: "12.87 per kg". */
function perUnit(fils: number, unit: Unit) {
  const u = unitOf(unit);
  return `${formatAmount(Math.round(fils * u.scale))} ${u.per}`;
}

type Editing = { kind: "product"; id: string } | { kind: "ingredient"; id: string | null } | undefined;
type ProductDraft = {
  name: string;
  code: string;
  area: Area;
  ingredientId: string;
  pack: string;
  /** The invoice's unit the pack is for, as EA or CAR; empty for any. */
  packUom: string;
  /** The other units it comes in, and what one of each holds. */
  packs: { uom: string; pack: string }[];
  newName: string;
  newUnit: Unit;
  /** With ingredientId SOLD: the menu item it is sold as. */
  soldAs: string;
};
const NEW = "new";
/** Sold as it is: the product is a menu item, one to one, such as a can. */
const SOLD = "sold";
const normalUom = (u: string) => u.trim().toUpperCase();

/** Ingredients: what recipes use, and the products that are each. */
export function IngredientsPage() {
  const [ingredients, setIngredients] = createSignal<Ingredient[]>();
  const [products, setProducts] = createSignal<Product[]>([]);
  const [loadError, setLoadError] = createSignal("");
  const load = async () => {
    try {
      const [i, p] = await Promise.all([call(api.GET("/v1/admin/ingredients")), call(api.GET("/v1/admin/products"))]);
      setIngredients(i);
      setProducts(p);
      setLoadError("");
    } catch (e) {
      setLoadError((e as Error).message);
    }
  };
  onMount(load);
  const unsorted = createMemo(() => products().filter((p) => !p.ingredientId));
  const productsOf = (id: string) => products().filter((p) => p.ingredientId === id);
  const ingredientOf = (id?: string) => ingredients()?.find((i) => i.id === id);

  const [editing, setEditing] = createSignal<Editing>();
  const [pd, setPd] = createStore<ProductDraft>({ name: "", code: "", area: "counter", ingredientId: "", pack: "", packUom: "",
    packs: [], newName: "", newUnit: "g", soldAs: "" });
  const menuItems = () => catalog()?.items ?? [];
  onMount(() => {
    if (!catalog()) void reloadCatalog();
  });
  const [id, setId] = createStore<{ name: string; unit: Unit }>({ name: "", unit: "g" });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const openProduct = (p: Product) => {
    setError(undefined);
    setStatus("");
    setPd({ name: p.name, code: p.code ?? "", area: p.area, ingredientId: p.ingredientId ?? "", pack: p.packAmount ? String(p.packAmount) : "",
      packUom: p.packAmount ? p.packUom : p.lastUom ?? "", packs: p.packs.map((x) => ({ uom: x.uom, pack: String(x.packAmount) })),
      newName: guessIngredientName(p.name), newUnit: suggestUnit(p.name), soldAs: "" });
    setEditing({ kind: "product", id: p.id });
  };
  const openIngredient = (x: Ingredient | null) => {
    setError(undefined);
    setStatus("");
    setId(x ? { name: x.name, unit: x.unit } : { name: "", unit: "g" });
    setEditing({ kind: "ingredient", id: x?.id ?? null });
  };
  const product = () => {
    const e = editing();
    return e?.kind === "product" ? products().find((p) => p.id === e.id) : undefined;
  };
  const ingredient = () => {
    const e = editing();
    return e?.kind === "ingredient" && e.id ? ingredientOf(e.id) : undefined;
  };

  // The product's unit: its ingredient's, or the new one's.
  const draftUnit = (): Unit | undefined =>
    pd.ingredientId === NEW ? pd.newUnit : pd.ingredientId === SOLD ? "piece" : ingredientOf(pd.ingredientId)?.unit;
  /** What the name suggests one holds, for a product not yet given a size. */
  const suggestPack = () => {
    if (product()?.packAmount) return;
    setPd("pack", String(packFor(pd.name, draftUnit(), pd.packUom) ?? ""));
  };
  const setSortTo = (to: string) => {
    setPd("ingredientId", to);
    suggestPack();
  };
  const packHint = () => {
    const unit = draftUnit();
    if (!unit) return "";
    if (!pd.pack.trim()) return `How much one holds, in ${unitOf(unit).label.toLowerCase()}. Type "48 x 170" as on the label.`;
    const n = parsePack(pd.pack);
    if (n === null) return "Type a number, or one like 48 x 170.";
    const p = product();
    let hint = `= ${amountOf(n, unit)}`;
    // The last purchase's price says something only if it was in this unit.
    const sameUnit = !normalUom(pd.packUom) || normalUom(pd.packUom) === (p?.lastUom ?? normalUom(pd.packUom));
    if (p?.lastCostEachFils && sameUnit)
      hint += `. At what one cost last, ${formatAmount(Math.round(p.lastCostEachFils))}, that's ${perUnit(p.lastCostEachFils / n, unit)}.`;
    return hint;
  };

  const run = async (fn: () => Promise<void>, done: string) => {
    setBusy(true);
    setError(undefined);
    setStatus("");
    try {
      await fn();
      await load();
      setStatus(done);
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };
  const saveProduct = () =>
    run(async () => {
      const p = product()!;
      let ingredientId: string | null = pd.ingredientId || null;
      const pack = pd.pack.trim() ? parsePack(pd.pack) : null;
      if (pd.pack.trim() && pack === null) throw { field: "packAmount", message: "Type a number, or one like 48 x 170." };
      const packs = pd.packs.filter((x) => x.uom.trim() || x.pack.trim()).map((x, i) => {
        const n = parsePack(x.pack);
        if (!x.uom.trim()) throw { field: `packs.${i}.uom`, message: "Name the unit, as CAR." };
        if (n === null) throw { field: `packs.${i}.packAmount`, message: "Type how much one holds, as 12000 or 6 x 2000." };
        return { uom: normalUom(x.uom), packAmount: n };
      });
      const item = pd.ingredientId === SOLD ? menuItems().find((it) => it.id === pd.soldAs) : undefined;
      if (pd.ingredientId === SOLD && !item) throw { field: "soldAs", message: "Choose the menu item it is sold as." };
      if (ingredientId === NEW || item) {
        const name = item ? item.name : pd.newName;
        const known = item && ingredients()?.find((x) => x.name.toLowerCase() === name.trim().toLowerCase());
        const made = known ?? await call(api.POST("/v1/admin/ingredients", { body: { name, unit: item ? "piece" : pd.newUnit } }))
          .catch((e) => {
            throw { field: item ? "soldAs" : "newName", message: (e as Error).message };
          });
        ingredientId = made.id;
        setPd({ ingredientId: made.id, newName: "", soldAs: "" });
      }
      await call(api.PUT("/v1/admin/products/{id}", {
        params: { path: { id: p.id } },
        body: { name: pd.name, code: pd.code, area: pd.area, ingredientId, packAmount: ingredientId ? pack : null,
          packUom: normalUom(pd.packUom), packs },
      }));
      // Sold as it is: the item's recipe is one of it, unless it has one.
      if (item && ingredientId && !item.costFromRecipe) {
        await call(api.PUT("/v1/admin/items/{id}/recipe", { params: { path: { id: item.id } },
          body: { lines: [{ ingredientId, amount: 1 }] } }));
        void reloadCatalog();
      }
    }, "Saved.");
  const saveIngredient = () =>
    run(async () => {
      const e = editing();
      const body = { name: id.name, unit: id.unit };
      if (e?.kind === "ingredient" && e.id) {
        await call(api.PUT("/v1/admin/ingredients/{id}", { params: { path: { id: e.id } }, body }));
      } else {
        const made = await call(api.POST("/v1/admin/ingredients", { body }));
        setEditing({ kind: "ingredient", id: made.id });
      }
    }, "Saved.");
  const removeIngredient = (x: Ingredient) =>
    run(async () => {
      await call(api.DELETE("/v1/admin/ingredients/{id}", { params: { path: { id: x.id } } }));
      setEditing(undefined);
    }, "");

  /**
   * How much one of the product's latest unit holds, so its latest price
   * reads per kg: its pack for that unit, else its main one where it goes.
   */
  const lastPack = (p: Product): number | null => {
    const u = p.lastUom ?? "";
    const own = u ? p.packs.find((x) => x.uom === u) : undefined;
    if (own) return own.packAmount;
    return p.packAmount != null && (!u || !p.packUom || p.packUom === u) ? p.packAmount : null;
  };
  const ProductRow = (props: { p: Product }) => {
    const unit = () => ingredientOf(props.p.ingredientId)?.unit;
    const pack = () => (unit() ? lastPack(props.p) : null);
    return (
      <button type="button" class="product-row" aria-pressed={editing()?.kind === "product" && editing()?.id === props.p.id}
        onClick={() => openProduct(props.p)}>
        <span>{props.p.name}</span>
        <span class="meta">
          <Show when={pack()} fallback={areaLabel(props.p.area)}>
            {amountOf(pack()!, unit()!)}
            <Show when={props.p.lastUom}> a {props.p.lastUom}</Show>
          </Show>
          <Show when={props.p.lastCostEachFils != null}>
            {" "}at {formatAmount(Math.round(props.p.lastCostEachFils!))}
            <Show when={pack()}>, {perUnit(props.p.lastCostEachFils! / pack()!, unit()!)}</Show>
          </Show>
        </span>
      </button>
    );
  };

  return (
    <div class="workspace ingredients">
      <section class="wall" aria-label="Ingredients">
        <div class="wall-head">
          <h1>Ingredients</h1>
          <span class="spacer" />
          <button type="button" class="btn btn-ink" onClick={() => openIngredient(null)}>
            Add an ingredient
          </button>
        </div>
        <p class="muted lede">
          What recipes use, such as evaporated milk, in grams, millilitres or pieces. Each product you buy is one ingredient
          and holds so much of it, so its price per kg comes from what you paid, whatever the brand.
        </p>
        <Show when={loadError()}>
          <p class="banner" role="alert">{loadError()}</p>
        </Show>

        <Show when={unsorted().length}>
          <section class="panel unsorted">
            <div class="panel-head">
              <h2>Not sorted yet</h2>
              <span class="meta">{unsorted().length === 1 ? "1 product" : `${unsorted().length} products`}</span>
            </div>
            <p class="meta">
              Give each the ingredient it is, and how much one holds. Leave out what recipes don't use, such as dish soap.
            </p>
            <div class="product-list">
              <For each={unsorted()}>{(p) => <ProductRow p={p} />}</For>
            </div>
          </section>
        </Show>

        <Show when={ingredients()} fallback={<Show when={!loadError()}><p class="loading">Opening the ingredients.</p></Show>}>
          <Show when={ingredients()!.length} fallback={
            <p class="empty">
              No ingredients yet. Record a purchase on <A href="/purchases">Purchases</A>, then sort its products here.
            </p>
          }>
            <div class="ingredient-list">
              <For each={ingredients()}>
                {(x) => (
                  <section class="ingredient" classList={{ on: editing()?.kind === "ingredient" && editing()?.id === x.id }}>
                    <button type="button" class="ingredient-head" onClick={() => openIngredient(x)}>
                      <strong>{x.name}</strong>
                      <Show when={x.unitCostFils != null} fallback={<span class="meta">No price yet</span>}>
                        <span class="price">{perUnit(x.unitCostFils!, x.unit)}</span>
                        <span class="meta">Average of the last 3 buys</span>
                      </Show>
                    </button>
                    <div class="product-list">
                      <For each={productsOf(x.id)} fallback={<span class="meta">No products yet.</span>}>
                        {(p) => <ProductRow p={p} />}
                      </For>
                    </div>
                  </section>
                )}
              </For>
            </div>
          </Show>
        </Show>
      </section>

      <aside class="editor" classList={{ closed: editing() === undefined }} aria-label="Editor">
        <Show when={editing()} fallback={
          <p class="editor-empty">Choose a product to sort it, or an ingredient to change it.</p>
        }>
          <Show when={editing()?.kind === "product" && product()} fallback={
            <>
              <EditorHead title={ingredient() ? ingredient()!.name : "New ingredient"} onClose={() => setEditing(undefined)} />
              <FormError error={error()} fields={["name", "unit"]} />
              <Field label="Name" value={id.name} placeholder="Evaporated milk" onInput={(v) => setId("name", v)}
                error={fieldError(error(), "name")} hint="What recipes call it, whatever the brand." />
              <fieldset class="field bare">
                <span>Measured in</span>
                <For each={units}>
                  {(u) => (
                    <label class="check">
                      <input type="radio" name="unit" checked={id.unit === u.id} onChange={() => setId("unit", u.id)} />
                      <span>{u.label}</span>
                    </label>
                  )}
                </For>
              </fieldset>
              <Show when={ingredient()}>
                {(x) => (
                  <p class="meta">
                    <Show when={x().unitCostFils != null} fallback="No price yet: buy one of its products with its pack size set.">
                      {perUnit(x().unitCostFils!, x().unit)}, from its last 3 purchases up to {shortDate(x().pricedOn!)}.
                    </Show>
                  </p>
                )}
              </Show>
              <div class="row">
                <button type="button" class="btn btn-ink" disabled={busy()} onClick={saveIngredient}>
                  {ingredient() ? "Save ingredient" : "Add ingredient"}
                </button>
                <span class="spacer" />
                <Show when={ingredient()}>
                  {(x) => (
                    <DeleteButton label="Delete" disabled={busy()} onDelete={() => removeIngredient(x())}
                      confirm={x().products ? `Delete; ${x().products} products stay, unsorted` : "Tap again to delete"} />
                  )}
                </Show>
              </div>
            </>
          }>
            <EditorHead title="Product" onClose={() => setEditing(undefined)} />
            <FormError error={error()} fields={["name", "area", "packAmount", "newName", "packUom", "soldAs",
              ...pd.packs.flatMap((_, i) => [`packs.${i}.uom`, `packs.${i}.packAmount`])]} />
            <Field label="Supplier's item code (optional)" value={pd.code} onInput={(v) => setPd("code", v)}
              error={fieldError(error(), "code")} hint="As on its invoices. A code typed on a purchase finds the product." />
            <Field label="Name, as invoices have it" value={pd.name} onInput={(v) => setPd("name", v)}
              error={fieldError(error(), "name")} />
            <label class="field">
              <span>Where it goes</span>
              <select class="input" value={pd.area} onChange={(e) => setPd("area", e.currentTarget.value as Area)}>
                <For each={areas}>{(a) => <option value={a.id} selected={pd.area === a.id}>{a.label}</option>}</For>
              </select>
            </label>
            <label class="field">
              <span>The ingredient it is</span>
              <select class="input" value={pd.ingredientId} onChange={(e) => setSortTo(e.currentTarget.value)}>
                <option value="">None: recipes don't use it</option>
                <For each={ingredients()}>{(x) => <option value={x.id} selected={pd.ingredientId === x.id}>{x.name}</option>}</For>
                <option value={NEW} selected={pd.ingredientId === NEW}>A new ingredient…</option>
                <option value={SOLD} selected={pd.ingredientId === SOLD}>Sold as it is: a menu item…</option>
              </select>
            </label>
            <Show when={pd.ingredientId === SOLD}>
              <label class="field">
                <span>The menu item</span>
                <select class="input" value={pd.soldAs} onChange={(e) => setPd("soldAs", e.currentTarget.value)}
                  aria-invalid={fieldError(error(), "soldAs") ? "true" : undefined}>
                  <option value="" selected={!pd.soldAs}>Choose…</option>
                  <For each={menuItems()}>{(it) => <option value={it.id} selected={pd.soldAs === it.id}>{it.name}</option>}</For>
                </select>
                <Show when={fieldError(error(), "soldAs")} fallback={
                  <span class="meta">
                    {menuItems().find((it) => it.id === pd.soldAs)?.costFromRecipe
                      ? "It has a recipe already, which stays. The new ingredient is named after it."
                      : "Saving makes an ingredient named after it, counted in pieces, and its recipe one of it."}
                  </span>
                }>
                  <span class="error">{fieldError(error(), "soldAs")}</span>
                </Show>
              </label>
            </Show>
            <Show when={pd.ingredientId === NEW}>
              <div class="mod">
                <Field label="New ingredient's name" value={pd.newName} placeholder="Evaporated milk"
                  onInput={(v) => setPd("newName", v)} error={fieldError(error(), "newName")} />
                <label class="field">
                  <span>Measured in</span>
                  <select class="input" value={pd.newUnit} onChange={(e) => {
                    setPd("newUnit", e.currentTarget.value as Unit);
                    suggestPack();
                  }}>
                    <For each={units}>{(u) => <option value={u.id} selected={pd.newUnit === u.id}>{u.label}</option>}</For>
                  </select>
                </label>
              </div>
            </Show>
            <Show when={draftUnit()}>
              <label class="field">
                <span>Unit on its invoices (optional)</span>
                <input class="input" list="uoms" placeholder="EA, CAR, PAC…" value={pd.packUom}
                  onInput={(e) => {
                    setPd("packUom", e.currentTarget.value);
                    suggestPack();
                  }}
                  aria-invalid={fieldError(error(), "packUom") ? "true" : undefined} />
                <span class="meta">As the invoice's UOM column says. The size below is for one of it.</span>
              </label>
              <datalist id="uoms">
                <For each={["EA", "CAR", "PAC", "BAG", "BOX", "CTN", "PCS", "KG", "BTL"]}>{(u) => <option value={u} />}</For>
              </datalist>
              <Field label={`${pd.packUom.trim() ? `One ${normalUom(pd.packUom)} holds` : "One holds"} (${unitOf(draftUnit()!).short})`}
                value={pd.pack} inputMode="decimal"
                onInput={(v) => setPd("pack", v)} error={fieldError(error(), "packAmount")} hint={packHint()} />
              <Show when={pd.packs.length}>
                <span class="meta">Other units it comes in</span>
              </Show>
              <For each={pd.packs}>
                {(x, i) => (
                  <div class="mod-row">
                    <label class="field">
                      <span class="visually-hidden">Unit</span>
                      <input class="input" list="uoms" placeholder="CAR" value={x.uom}
                        onInput={(e) => setPd("packs", i(), "uom", e.currentTarget.value)}
                        aria-invalid={fieldError(error(), `packs.${i()}.uom`) ? "true" : undefined} />
                    </label>
                    <label class="field">
                      <span class="visually-hidden">One holds</span>
                      <input class="input" inputmode="decimal" placeholder={`One holds (${unitOf(draftUnit()!).short})`} value={x.pack}
                        onInput={(e) => setPd("packs", i(), "pack", e.currentTarget.value)}
                        aria-invalid={fieldError(error(), `packs.${i()}.packAmount`) ? "true" : undefined} />
                    </label>
                    <button type="button" class="btn btn-danger btn-small"
                      onClick={() => setPd("packs", (ps) => ps.filter((_, j) => j !== i()))}>
                      Remove
                    </button>
                  </div>
                )}
              </For>
              <Show when={pd.pack.trim()}>
                <button type="button" class="btn btn-option" onClick={() => setPd("packs", (ps) => [...ps, { uom: "", pack: "" }])}>
                  Add another unit
                </button>
              </Show>
            </Show>
            <Show when={product()?.lastPurchasedOn}>
              <p class="meta">
                Last bought {shortDate(product()!.lastPurchasedOn!)}: {product()!.lastQuantity} at{" "}
                {formatAmount(Math.round(product()!.lastCostEachFils ?? 0))} each, as paid.
              </p>
            </Show>
            <div class="row">
              <button type="button" class="btn btn-ink" disabled={busy()} onClick={saveProduct}>
                Save product
              </button>
            </div>
          </Show>
          <p class="status" role="status">{status()}</p>
        </Show>
      </aside>
    </div>
  );
}
