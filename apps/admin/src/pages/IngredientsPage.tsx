import { A } from "@solidjs/router";
import { createMemo, createSignal, For, onMount, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { api, call, type Area, type Ingredient, type Product, type Unit } from "../api/client";
import { areaLabel, areas, unitOf, units } from "../areas";
import { shortDate } from "../dates";
import { parsePack } from "../guess";
import { formatAmount } from "../money";
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
  newName: string;
  newUnit: Unit;
};
const NEW = "new";

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
  const [pd, setPd] = createStore<ProductDraft>({ name: "", code: "", area: "counter", ingredientId: "", pack: "", newName: "", newUnit: "g" });
  const [id, setId] = createStore<{ name: string; unit: Unit }>({ name: "", unit: "g" });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const openProduct = (p: Product) => {
    setError(undefined);
    setStatus("");
    setPd({ name: p.name, code: p.code ?? "", area: p.area, ingredientId: p.ingredientId ?? "", pack: p.packAmount ? String(p.packAmount) : "",
      newName: "", newUnit: "g" });
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
    pd.ingredientId === NEW ? pd.newUnit : ingredientOf(pd.ingredientId)?.unit;
  const packHint = () => {
    const unit = draftUnit();
    if (!unit) return "";
    if (!pd.pack.trim()) return `How much one holds, in ${unitOf(unit).label.toLowerCase()}. Type "48 x 170" as on the label.`;
    const n = parsePack(pd.pack);
    if (n === null) return "Type a number, or one like 48 x 170.";
    const p = product();
    let hint = `= ${amountOf(n, unit)}`;
    if (p?.lastCostEachFils) hint += `. At what one cost last, ${formatAmount(Math.round(p.lastCostEachFils))}, that's ${perUnit(p.lastCostEachFils / n, unit)}.`;
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
      if (ingredientId === NEW) {
        const made = await call(api.POST("/v1/admin/ingredients", { body: { name: pd.newName, unit: pd.newUnit } }))
          .catch((e) => {
            throw { field: "newName", message: (e as Error).message };
          });
        ingredientId = made.id;
        setPd({ ingredientId: made.id, newName: "" });
      }
      await call(api.PUT("/v1/admin/products/{id}", {
        params: { path: { id: p.id } },
        body: { name: pd.name, code: pd.code, area: pd.area, ingredientId, packAmount: ingredientId ? pack : null },
      }));
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

  const ProductRow = (props: { p: Product }) => (
    <button type="button" class="product-row" aria-pressed={editing()?.kind === "product" && editing()?.id === props.p.id}
      onClick={() => openProduct(props.p)}>
      <span>{props.p.name}</span>
      <span class="meta">
        <Show when={props.p.packAmount && ingredientOf(props.p.ingredientId)} fallback={areaLabel(props.p.area)}>
          {amountOf(props.p.packAmount!, ingredientOf(props.p.ingredientId)!.unit)}
        </Show>
        <Show when={props.p.lastCostEachFils != null}>
          {" "}at {formatAmount(Math.round(props.p.lastCostEachFils!))}
          <Show when={props.p.packAmount && ingredientOf(props.p.ingredientId)}>
            , {perUnit(props.p.lastCostEachFils! / props.p.packAmount!, ingredientOf(props.p.ingredientId)!.unit)}
          </Show>
        </Show>
      </span>
    </button>
  );

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
            <FormError error={error()} fields={["name", "area", "packAmount", "newName"]} />
            <Field label="Supplier's item code (optional)" value={pd.code} onInput={(v) => setPd("code", v)}
              error={fieldError(error(), "code")} hint="As on its invoices. A code typed on a purchase finds the product." />
            <Field label="Name, as invoices have it" value={pd.name} onInput={(v) => setPd("name", v)}
              error={fieldError(error(), "name")} />
            <label class="field">
              <span>Where it goes</span>
              <select class="input" value={pd.area} onChange={(e) => setPd("area", e.currentTarget.value as Area)}>
                <For each={areas}>{(a) => <option value={a.id}>{a.label}</option>}</For>
              </select>
            </label>
            <label class="field">
              <span>The ingredient it is</span>
              <select class="input" value={pd.ingredientId} onChange={(e) => setPd("ingredientId", e.currentTarget.value)}>
                <option value="">None: recipes don't use it</option>
                <For each={ingredients()}>{(x) => <option value={x.id}>{x.name}</option>}</For>
                <option value={NEW}>A new ingredient…</option>
              </select>
            </label>
            <Show when={pd.ingredientId === NEW}>
              <div class="mod">
                <Field label="New ingredient's name" value={pd.newName} placeholder="Evaporated milk"
                  onInput={(v) => setPd("newName", v)} error={fieldError(error(), "newName")} />
                <label class="field">
                  <span>Measured in</span>
                  <select class="input" value={pd.newUnit} onChange={(e) => setPd("newUnit", e.currentTarget.value as Unit)}>
                    <For each={units}>{(u) => <option value={u.id}>{u.label}</option>}</For>
                  </select>
                </label>
              </div>
            </Show>
            <Show when={draftUnit()}>
              <Field label={`One holds (${unitOf(draftUnit()!).short})`} value={pd.pack} inputMode="decimal"
                onInput={(v) => setPd("pack", v)} error={fieldError(error(), "packAmount")} hint={packHint()} />
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
