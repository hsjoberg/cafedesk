import { A } from "@solidjs/router";
import { createEffect, createMemo, createSignal, For, Match, onMount, Show, Switch } from "solid-js";
import { createStore } from "solid-js/store";
import { api, call, type Category, type Item } from "../api/client";
import { Tile } from "../components/Tile";
import { formatFils, parseAmount } from "../money";
import { itemRecipeLines, loadRecipes, recipeText } from "../recipeText";
import { catalog, catalogError, reloadCatalog } from "../state";
import { DeleteButton, EditorHead, Field, fieldError, FormError, toSaveError, type SaveError } from "../ui";

/** The signage colors a category can have, as on the till. */
export const bandColors = [
  { hex: "#5F8A3A", name: "Cardamom" },
  { hex: "#2F6690", name: "Harbour" },
  { hex: "#7B4B94", name: "Plum" },
  { hex: "#1E8A83", name: "Teal" },
  { hex: "#8C6A1C", name: "Date" },
  { hex: "#A3366B", name: "Rose" },
  { hex: "#4F5D75", name: "Slate" },
  { hex: "#6B7A2A", name: "Olive" },
];

type Editing = { kind: "item"; id: string | null } | { kind: "category"; id: string | null } | null;

type ItemDraft = {
  name: string;
  nameAr: string;
  price: string;
  /** What one costs the shop, in dirhams; "" for not known. */
  cost: string;
  /** The cost it had when opened, to say what a change does. */
  savedCost: number | null;
  categoryId: string;
  kitchen: boolean;
  groupIds: string[];
};

type CategoryDraft = { name: string; nameAr: string; color: string; kitchen: boolean };

export function MenuPage() {
  onMount(() => {
    void reloadCatalog();
    void loadRecipes();
  });
  const categories = () => catalog()?.categories ?? [];
  const groups = () => catalog()?.modifierGroups ?? [];
  const [categoryId, setCategoryId] = createSignal<string>();
  createEffect(() => {
    if (!categories().some((c) => c.id === categoryId())) setCategoryId(categories()[0]?.id);
  });
  const category = () => categories().find((c) => c.id === categoryId());
  const items = createMemo(() => (catalog()?.items ?? []).filter((i) => i.categoryId === categoryId()));

  const [editing, setEditing] = createSignal<Editing>(null);
  const [item, setItem] = createStore<ItemDraft>(emptyItem(undefined));
  const [cat, setCat] = createStore<CategoryDraft>({ name: "", nameAr: "", color: bandColors[0]!.hex, kitchen: false });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  function emptyItem(c: Category | undefined): ItemDraft {
    return { name: "", nameAr: "", price: "", cost: "", savedCost: null, categoryId: c?.id ?? "",
      kitchen: c?.kitchen ?? false, groupIds: [] };
  }

  const reset = () => {
    setError(undefined);
    setStatus("");
  };
  const editItem = (it: Item | null) => {
    reset();
    setItem(
      it
        ? { name: it.name, nameAr: it.nameAr ?? "", price: formatFils(it.priceFils),
            cost: it.costFils == null ? "" : formatFils(it.costFils), savedCost: it.costFils ?? null,
            categoryId: it.categoryId, kitchen: it.kitchen, groupIds: [...it.modifierGroupIds] }
        : emptyItem(category()),
    );
    setEditing({ kind: "item", id: it?.id ?? null });
  };
  const editCategory = (c: Category | null) => {
    reset();
    setCat(c ? { name: c.name, nameAr: c.nameAr ?? "", color: c.color, kitchen: c.kitchen }
      : { name: "", nameAr: "", color: bandColors[categories().length % bandColors.length]!.hex, kitchen: false });
    setEditing({ kind: "category", id: c?.id ?? null });
  };
  // What the cost field means for this item, as it stands.
  const costHint = () => {
    const price = parseAmount(item.price);
    const cost = item.cost.trim() === "" ? null : parseAmount(item.cost);
    if (item.cost.trim() === "")
      return item.savedCost === null
        ? "Only you see it, never the till. With it, Sales shows what this item makes."
        : "Empty, later sales of it show no profit. Earlier ones keep their cost.";
    if (cost === null) return "In dirhams, like 0.45.";
    const parts: string[] = [];
    if (price !== null && price > 0) {
      const profit = price - cost;
      parts.push(`${formatFils(profit)} profit on each, ${Math.round((profit / price) * 100)}% of the price, before options.`);
    }
    if (editing()?.id && cost !== item.savedCost)
      parts.push(item.savedCost === null
        ? "It counts for earlier sales too, as they had no cost."
        : `Sales from now on use it; earlier ones keep ${formatFils(item.savedCost)}.`);
    return parts.join(" ");
  };
  const close = () => {
    reset();
    setEditing(null);
  };
  const editingItem = (id: string) => {
    const e = editing();
    return e?.kind === "item" && e.id === id;
  };

  // Runs a save, showing its outcome.
  const save = async (fn: () => Promise<void>) => {
    setBusy(true);
    reset();
    try {
      await fn();
      await reloadCatalog();
      setStatus("Saved.");
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const saveItem = () =>
    save(async () => {
      const priceFils = parseAmount(item.price);
      if (priceFils === null) throw { field: "priceFils", message: "Enter a price like 2.50." };
      const costFils = item.cost.trim() === "" ? null : parseAmount(item.cost);
      if (costFils === null && item.cost.trim() !== "")
        throw { field: "costFils", message: "Enter a cost like 0.45, or leave it empty." };
      const order = groups().map((g) => g.id);
      const body = {
        categoryId: item.categoryId,
        name: item.name,
        nameAr: item.nameAr,
        priceFils,
        costFils,
        kitchen: item.kitchen,
        modifierGroupIds: [...item.groupIds].sort((a, b) => order.indexOf(a) - order.indexOf(b)),
      };
      const e = editing();
      const saved =
        e?.kind === "item" && e.id
          ? await call(api.PUT("/v1/admin/items/{id}", { params: { path: { id: e.id } }, body }))
          : await call(api.POST("/v1/admin/items", { body }));
      setEditing({ kind: "item", id: saved.id });
      setCategoryId(saved.categoryId);
    });

  const saveCategory = () =>
    save(async () => {
      const body = { name: cat.name, nameAr: cat.nameAr, color: cat.color, kitchen: cat.kitchen };
      const e = editing();
      const saved =
        e?.kind === "category" && e.id
          ? await call(api.PUT("/v1/admin/categories/{id}", { params: { path: { id: e.id } }, body }))
          : await call(api.POST("/v1/admin/categories", { body }));
      setEditing({ kind: "category", id: saved.id });
      setCategoryId(saved.id);
    });

  const remove = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    reset();
    try {
      await fn();
      await reloadCatalog();
      setEditing(null);
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const move = (kind: "item" | "category", id: string, position: number) =>
    save(async () => {
      if (kind === "item") await call(api.POST("/v1/admin/items/{id}/move", { params: { path: { id } }, body: { position } }));
      else await call(api.POST("/v1/admin/categories/{id}/move", { params: { path: { id } }, body: { position } }));
    });

  const editedItem = () => {
    const e = editing();
    return e?.kind === "item" && e.id ? (catalog()?.items ?? []).find((i) => i.id === e.id) : undefined;
  };
  const editedCategory = () => {
    const e = editing();
    return e?.kind === "category" && e.id ? categories().find((c) => c.id === e.id) : undefined;
  };
  const draftColor = () => categories().find((c) => c.id === item.categoryId)?.color ?? category()?.color ?? "#56675F";

  return (
    <div class="workspace">
      <section class="wall" aria-label="Menu">
        <Show when={catalogError()}>
          <p class="banner" role="alert">{catalogError()}</p>
        </Show>
        <div class="tabs" role="tablist" aria-label="Categories">
          <For each={categories()}>
            {(c) => (
              <button type="button" class="tab" role="tab" aria-selected={c.id === categoryId()}
                style={{ "--band": editedCategory()?.id === c.id ? cat.color : c.color }}
                onClick={() => setCategoryId(c.id)}>
                <span class="swatch" />
                {editedCategory()?.id === c.id ? cat.name || c.name : c.name}
              </button>
            )}
          </For>
          <button type="button" class="btn btn-option" onClick={() => editCategory(null)}>
            Add a category
          </button>
        </div>

        <Show when={category()} fallback={
          <Show when={catalog()}>
            <p class="empty">The menu is empty. Add a category, such as Tea, then add its items.</p>
          </Show>
        }>
          {(c) => (
            <>
              <div class="wall-head">
                <h1>{c().name}</h1>
                <button type="button" class="btn btn-option" onClick={() => editCategory(c())}>
                  Edit category
                </button>
                <span class="spacer" />
                <button type="button" class="btn btn-ink" onClick={() => editItem(null)}>
                  Add an item
                </button>
              </div>
              <div class="tiles">
                <For each={items()}>
                  {(it) => (
                    <Show when={editingItem(it.id)} fallback={
                      <Tile name={it.name} nameAr={it.nameAr ?? ""} priceFils={it.priceFils} color={c().color}
                        onClick={() => editItem(it)} />
                    }>
                      <Tile name={item.name} nameAr={item.nameAr} priceFils={parseAmount(item.price)}
                        color={draftColor()} selected onClick={() => editItem(it)} />
                    </Show>
                  )}
                </For>
                <Show when={editing()?.kind === "item" && editing()?.id === null && item.categoryId === c().id}>
                  <Tile name={item.name} nameAr={item.nameAr} priceFils={parseAmount(item.price)} color={c().color} draft />
                </Show>
              </div>
              <Show when={items().length === 0 && !(editing()?.kind === "item" && editing()?.id === null)}>
                <p class="empty">No items in {c().name} yet. Add one.</p>
              </Show>
            </>
          )}
        </Show>
      </section>

      <aside class="editor" classList={{ closed: editing() === null }} aria-label="Editor">
        <Switch fallback={
          <p class="editor-empty">Choose an item to change it, or add one. The tile shows your changes as you type.</p>
        }>
          <Match when={editing()?.kind === "item"}>
            <EditorHead title={editing()?.id ? "Edit item" : "New item"} onClose={close} />
            <div class="tiles narrow-preview" aria-hidden="true">
              <Tile name={item.name} nameAr={item.nameAr} priceFils={parseAmount(item.price)} color={draftColor()} />
            </div>
            <FormError error={error()} fields={["name", "nameAr", "priceFils", "costFils"]} />
            <Field label="Name" value={item.name} onInput={(v) => setItem("name", v)} error={fieldError(error(), "name")} />
            <Field label="Arabic name" value={item.nameAr} rtl onInput={(v) => setItem("nameAr", v)}
              error={fieldError(error(), "nameAr")} />
            <Field label="Price" value={item.price} inputMode="decimal" placeholder="2.50"
              onInput={(v) => setItem("price", v)} error={fieldError(error(), "priceFils")} hint="In dirhams" />
            <Show when={editedItem()?.costFromRecipe} fallback={
              <Field label="Cost to make or buy (optional)" value={item.cost} inputMode="decimal"
                onInput={(v) => setItem("cost", v)} error={fieldError(error(), "costFils")} hint={costHint()} />
            }>
              <div class="field">
                <span>Cost, from its recipe</span>
                <p class="from-recipe">
                  {editedItem()!.costFils != null ? formatFils(editedItem()!.costFils!) : "No cost yet: an ingredient has no price"}
                </p>
                <Show when={recipeText(itemRecipeLines(editedItem()!.id)).length}>
                  <ul class="recipe-origin">
                    <For each={recipeText(itemRecipeLines(editedItem()!.id))}>{(t) => <li>{t}</li>}</For>
                  </ul>
                </Show>
                <span class="meta">
                  It follows the recipe and today's prices. Change it on <A href="/recipes">Recipes</A>.
                </span>
              </div>
            </Show>
            <label class="field">
              <span>Category</span>
              <select class="input" value={item.categoryId} onChange={(e) => setItem("categoryId", e.currentTarget.value)}>
                <For each={categories()}>{(c) => <option value={c.id}>{c.name}</option>}</For>
              </select>
            </label>
            <fieldset class="field" style={{ border: "0", padding: "0", margin: "0" }}>
              <span>Options the cashier can choose</span>
              <Show when={groups().length > 0} fallback={<span class="meta">No option groups yet. Add them under Options.</span>}>
                <For each={groups()}>
                  {(g) => (
                    <label class="check">
                      <input type="checkbox" checked={item.groupIds.includes(g.id)}
                        onChange={(e) =>
                          setItem("groupIds", e.currentTarget.checked
                            ? [...item.groupIds, g.id] : item.groupIds.filter((id) => id !== g.id))} />
                      <span>
                        {g.name}
                        <span class="meta"> {g.modifiers.map((m) => m.name).join(", ")}</span>
                      </span>
                    </label>
                  )}
                </For>
              </Show>
            </fieldset>
            <label class="check">
              <input type="checkbox" checked={item.kitchen} onChange={(e) => setItem("kitchen", e.currentTarget.checked)} />
              <span>Print on the kitchen ticket</span>
            </label>
            <Show when={editedItem()}>
              {(it) => (
                <div class="row">
                  <button type="button" class="btn btn-option btn-small" disabled={busy() || it().position === 0}
                    onClick={() => move("item", it().id, it().position - 1)}>Move earlier</button>
                  <button type="button" class="btn btn-option btn-small" disabled={busy() || it().position >= items().length - 1}
                    onClick={() => move("item", it().id, it().position + 1)}>Move later</button>
                </div>
              )}
            </Show>
            <div class="row">
              <button type="button" class="btn btn-ink" disabled={busy()} onClick={saveItem}>
                {editing()?.id ? "Save item" : "Add item"}
              </button>
              <span class="spacer" />
              <Show when={editedItem()}>
                {(it) => (
                  <DeleteButton label="Delete item" confirm="Tap again to delete" disabled={busy()}
                    onDelete={() => remove(() => call(api.DELETE("/v1/admin/items/{id}", { params: { path: { id: it().id } } })))} />
                )}
              </Show>
            </div>
            <p class="status" role="status">{status()}</p>
          </Match>

          <Match when={editing()?.kind === "category"}>
            <EditorHead title={editing()?.id ? "Edit category" : "New category"} onClose={close} />
            <FormError error={error()} fields={["name", "nameAr", "color"]} />
            <Field label="Name" value={cat.name} onInput={(v) => setCat("name", v)} error={fieldError(error(), "name")} />
            <Field label="Arabic name" value={cat.nameAr} rtl onInput={(v) => setCat("nameAr", v)}
              error={fieldError(error(), "nameAr")} />
            <div class="field">
              <span>Color of the band on its tiles</span>
              <div class="swatches" role="radiogroup" aria-label="Color">
                <For each={bandColors}>
                  {(b) => (
                    <button type="button" role="radio" aria-checked={cat.color.toUpperCase() === b.hex}
                      aria-label={b.name} title={b.name} style={{ "--band": b.hex }} onClick={() => setCat("color", b.hex)} />
                  )}
                </For>
              </div>
            </div>
            <label class="check">
              <input type="checkbox" checked={cat.kitchen} onChange={(e) => setCat("kitchen", e.currentTarget.checked)} />
              <span>New items print on the kitchen ticket</span>
            </label>
            <Show when={editedCategory()}>
              {(c) => (
                <div class="row">
                  <button type="button" class="btn btn-option btn-small" disabled={busy() || c().position === 0}
                    onClick={() => move("category", c().id, c().position - 1)}>Move earlier</button>
                  <button type="button" class="btn btn-option btn-small" disabled={busy() || c().position >= categories().length - 1}
                    onClick={() => move("category", c().id, c().position + 1)}>Move later</button>
                </div>
              )}
            </Show>
            <div class="row">
              <button type="button" class="btn btn-ink" disabled={busy()} onClick={saveCategory}>
                {editing()?.id ? "Save category" : "Add category"}
              </button>
              <span class="spacer" />
              <Show when={editedCategory()}>
                {(c) => (
                  <DeleteButton label="Delete category" confirm="Tap again to delete" disabled={busy()}
                    onDelete={() => remove(() => call(api.DELETE("/v1/admin/categories/{id}", { params: { path: { id: c().id } } })))} />
                )}
              </Show>
            </div>
            <p class="status" role="status">{status()}</p>
          </Match>
        </Switch>
      </aside>
    </div>
  );
}
