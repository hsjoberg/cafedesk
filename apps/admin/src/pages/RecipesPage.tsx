import { A } from "@solidjs/router";
import { createMemo, createSignal, For, Index, onMount, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { api, call, type Ingredient, type Item, type Modifier, type RecipeLine, type Recipes } from "../api/client";
import { unitOf } from "../areas";
import { formatAmount, formatFils } from "../money";
import { catalog, catalogError, reloadCatalog } from "../state";
import { DeleteButton, EditorHead, Field, fieldError, FormError, toSaveError, type SaveError } from "../ui";

type Editing = { kind: "item" | "option"; id: string } | { kind: "batch"; id: string | null } | undefined;
/** A recipe line as typed: "i:<ingredient>" or "b:<batch>", and an amount. */
type LineDraft = { ref: string; amount: string };

const grouped = new Intl.NumberFormat("en", { maximumFractionDigits: 3 });

/** Reads an amount, up to 3 decimals; below 0 only where allowed. */
function parseAmountOf(text: string, negative: boolean): number | null {
  const m = /^\s*(-?)(\d{0,7})(?:\.(\d{1,3}))?\s*$/.exec(text.replace(/,/g, ""));
  if (!m || (m[2] === "" && !m[3]) || (m[1] && !negative)) return null;
  const n = Number(`${m[1]}${m[2] || "0"}.${m[3] || "0"}`);
  return n === 0 ? null : n;
}

const toDraft = (lines: RecipeLine[]): LineDraft[] =>
  lines.map((l) => ({ ref: l.ingredientId ? `i:${l.ingredientId}` : `b:${l.batchId}`, amount: grouped.format(l.amount).replace(/,/g, "") }));

/** Recipes: what goes into each item, option and batch, and what that costs. */
export function RecipesPage() {
  const [recipes, setRecipes] = createSignal<Recipes>();
  const [ingredients, setIngredients] = createSignal<Ingredient[]>([]);
  const [loadError, setLoadError] = createSignal("");
  const load = async () => {
    try {
      const [r, i] = await Promise.all([call(api.GET("/v1/admin/recipes")), call(api.GET("/v1/admin/ingredients")), reloadCatalog()]);
      setRecipes(r);
      setIngredients(i);
      setLoadError("");
    } catch (e) {
      setLoadError((e as Error).message);
    }
  };
  onMount(load);

  const categories = () => catalog()?.categories ?? [];
  const items = () => catalog()?.items ?? [];
  const groups = () => catalog()?.modifierGroups ?? [];
  const batches = () => recipes()?.batches ?? [];
  const itemRecipe = (id: string) => recipes()?.items.find((r) => r.itemId === id);
  const modRecipe = (id: string) => recipes()?.modifiers.find((r) => r.modifierId === id);
  const ingredient = (id: string) => ingredients().find((i) => i.id === id);
  const batch = (id: string) => batches().find((b) => b.id === id);

  // The editor.
  const [editing, setEditing] = createSignal<Editing>();
  const [lines, setLines] = createStore<{ list: LineDraft[] }>({ list: [] });
  const [bd, setBd] = createStore({ name: "", portions: "" });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const open = (e: Editing) => {
    setError(undefined);
    setStatus("");
    let l: RecipeLine[] = [];
    if (e?.kind === "item") l = itemRecipe(e.id)?.lines ?? [];
    if (e?.kind === "option") l = modRecipe(e.id)?.lines ?? [];
    if (e?.kind === "batch") {
      const b = e.id ? batch(e.id) : undefined;
      l = b?.lines ?? [];
      setBd({ name: b?.name ?? "", portions: b ? grouped.format(b.yieldPortions).replace(/,/g, "") : "" });
    }
    setLines("list", l.length ? toDraft(l) : [{ ref: "", amount: "" }]);
    setEditing(e);
  };
  const editedItem = (): Item | undefined => {
    const e = editing();
    return e?.kind === "item" ? items().find((i) => i.id === e.id) : undefined;
  };
  const editedOption = (): (Modifier & { group: string }) | undefined => {
    const e = editing();
    if (e?.kind !== "option") return undefined;
    for (const g of groups()) {
      const m = g.modifiers.find((x) => x.id === e.id);
      if (m) return { ...m, group: g.name };
    }
    return undefined;
  };
  const negative = () => editing()?.kind === "option";

  // What a line costs now, in fils; null without a price.
  /** A line's unit, as its amount reads: "g", "1 piece", "2 portions". */
  const unitOfRef = (ref: string, amount: string) => {
    const one = Math.abs(Number(amount.replace(/,/g, ""))) === 1;
    if (ref.startsWith("i:")) {
      const i = ingredient(ref.slice(2));
      if (!i) return "";
      return i.unit === "piece" && one ? "piece" : unitOf(i.unit).short;
    }
    return ref.startsWith("b:") ? (one ? "portion" : "portions") : "";
  };
  const lineCost = (l: LineDraft): { fils: number | null; missing?: string } => {
    const n = parseAmountOf(l.amount, negative());
    if (n === null || !l.ref) return { fils: 0 };
    if (l.ref.startsWith("i:")) {
      const i = ingredient(l.ref.slice(2));
      if (!i) return { fils: 0 };
      return i.unitCostFils != null ? { fils: n * i.unitCostFils } : { fils: null, missing: i.name };
    }
    const b = batch(l.ref.slice(2));
    if (!b) return { fils: 0 };
    return b.portionCostFils != null ? { fils: n * b.portionCostFils } : { fils: null, missing: b.name };
  };
  const total = createMemo(() => {
    let fils = 0;
    const missing: string[] = [];
    for (const l of lines.list) {
      const c = lineCost(l);
      if (c.fils === null) missing.push(c.missing!);
      else fils += c.fils;
    }
    return { fils: Math.round(fils), missing };
  });

  const toLines = (): RecipeLine[] => {
    const kept = lines.list.filter((l) => l.ref || l.amount.trim());
    if (kept.length !== lines.list.length) setLines("list", kept.length ? kept : [{ ref: "", amount: "" }]);
    return kept.map((l, i) => {
      if (!l.ref) throw { field: `lines.${i}`, message: "Choose an ingredient or batch for each line." };
      const amount = parseAmountOf(l.amount, negative());
      if (amount === null)
        throw { field: `lines.${i}.amount`, message: negative() ? "Enter how much, as 5 or -10." : "Enter how much, as 40 or 0.5." };
      return l.ref.startsWith("i:") ? { ingredientId: l.ref.slice(2), amount } : { batchId: l.ref.slice(2), amount };
    });
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
  const save = () =>
    run(async () => {
      const e = editing();
      if (!e) return;
      const body = { lines: toLines() };
      if (e.kind === "item") await call(api.PUT("/v1/admin/items/{id}/recipe", { params: { path: { id: e.id } }, body }));
      else if (e.kind === "option") await call(api.PUT("/v1/admin/modifiers/{id}/recipe", { params: { path: { id: e.id } }, body }));
      else {
        const portions = parseAmountOf(bd.portions, false);
        if (portions === null) throw { field: "yieldPortions", message: "Enter how many portions a batch makes." };
        const b = { name: bd.name, yieldPortions: portions, lines: body.lines };
        if (e.id) await call(api.PUT("/v1/admin/batches/{id}", { params: { path: { id: e.id } }, body: b }));
        else {
          const made = await call(api.POST("/v1/admin/batches", { body: b }));
          setEditing({ kind: "batch", id: made.id });
        }
      }
    }, "Saved.");
  const removeRecipe = () =>
    run(async () => {
      const e = editing();
      if (e?.kind === "item") await call(api.PUT("/v1/admin/items/{id}/recipe", { params: { path: { id: e.id } }, body: { lines: [] } }));
      if (e?.kind === "option") await call(api.PUT("/v1/admin/modifiers/{id}/recipe", { params: { path: { id: e.id } }, body: { lines: [] } }));
      setLines("list", [{ ref: "", amount: "" }]);
    }, "Recipe removed.");
  const removeBatch = (id: string) =>
    run(async () => {
      await call(api.DELETE("/v1/admin/batches/{id}", { params: { path: { id } } }));
      setEditing(undefined);
    }, "");

  const costCell = (cost: number | null | undefined, fromRecipe: boolean) => (
    <Show when={cost != null} fallback={<span class="muted">{fromRecipe ? "No price yet" : "No cost"}</span>}>
      {formatFils(cost!)}
    </Show>
  );
  const margin = (price: number, cost: number | null | undefined) =>
    cost != null && price > 0 ? `${Math.round(((price - cost) / price) * 100)}%` : "";
  /** The items and options whose recipes use a batch, by name. */
  const usersOf = (batchId: string) => {
    const out: { kind: "item" | "option"; id: string; name: string }[] = [];
    for (const r of recipes()?.items ?? []) {
      if (r.lines.some((l) => l.batchId === batchId)) {
        const it = items().find((i) => i.id === r.itemId);
        if (it) out.push({ kind: "item", id: it.id, name: it.name });
      }
    }
    for (const r of recipes()?.modifiers ?? []) {
      if (r.lines.some((l) => l.batchId === batchId)) {
        for (const g of groups()) {
          const m = g.modifiers.find((x) => x.id === r.modifierId);
          if (m) out.push({ kind: "option", id: m.id!, name: `${m.name} (${g.name})` });
        }
      }
    }
    return out;
  };
  /** The batches an item's recipe uses, by name. */
  const batchesOf = (itemId: string) =>
    (itemRecipe(itemId)?.lines ?? []).filter((l) => l.batchId).map((l) => batch(l.batchId!)?.name).filter(Boolean);
  const isEditing = (kind: string, id: string) => {
    const e = editing();
    return e?.kind === kind && e.id === id;
  };

  return (
    <div class="workspace recipes">
      <section class="wall" aria-label="Recipes">
        <div class="wall-head">
          <h1>Recipes</h1>
        </div>
        <p class="muted lede">
          What goes into each item, from ingredients and the chef's batches. An item with a recipe gets its cost from it,
          at today's prices, and follows them as purchases come in. Past sales keep the cost they had.
        </p>
        <Show when={loadError() || catalogError()}>
          <p class="banner" role="alert">{loadError() || catalogError()}</p>
        </Show>

        <section class="panel">
          <div class="panel-head">
            <h2>Batches</h2>
            <span class="spacer" />
            <button type="button" class="btn btn-option btn-small" onClick={() => open({ kind: "batch", id: null })}>
              Add a batch
            </button>
          </div>
          <p class="meta">The chef's big cook: what goes in the pot, and how many portions it makes. Meals use portions of it.</p>
          <Show when={batches().length} fallback={<p class="empty">No batches yet. Add one for each pot the chef makes.</p>}>
            <div class="batch-list">
              <For each={batches()}>
                {(b) => (
                  <button type="button" class="batch" aria-pressed={isEditing("batch", b.id)}
                    onClick={() => open({ kind: "batch", id: b.id })}>
                    <strong>{b.name}</strong>
                    <span class="price">
                      {b.portionCostFils != null ? `${formatFils(Math.round(b.portionCostFils))} a portion` : "No price yet"}
                    </span>
                    <span class="meta">Makes {grouped.format(b.yieldPortions)} portions.</span>
                    <span class="meta used-in">
                      {usersOf(b.id).length ? `Used in ${usersOf(b.id).map((u) => u.name).join(", ")}.` : "No meal uses it yet."}
                    </span>
                  </button>
                )}
              </For>
            </div>
          </Show>
        </section>

        <For each={categories()}>
          {(c) => (
            <section class="panel">
              <h2>{c.name}</h2>
              <div class="table-wrap"><table class="table recipe-table">
                <thead>
                  <tr>
                    <th scope="col">Item</th>
                    <th scope="col" class="n">Price</th>
                    <th scope="col" class="n">Cost</th>
                    <th scope="col" class="n">Margin</th>
                    <th scope="col">From</th>
                  </tr>
                </thead>
                <tbody>
                  <For each={items().filter((i) => i.categoryId === c.id)}>
                    {(it) => (
                      <tr classList={{ on: isEditing("item", it.id) }} onClick={() => open({ kind: "item", id: it.id })}>
                        <th scope="row">
                          <button type="button" class="link-row" onClick={(e) => {
                            e.stopPropagation();
                            open({ kind: "item", id: it.id });
                          }}>{it.name}</button>
                          <Show when={batchesOf(it.id).length}>
                            <span class="meta from-batches">From {batchesOf(it.id).join(" and ")}</span>
                          </Show>
                        </th>
                        <td class="n">{formatFils(it.priceFils)}</td>
                        <td class="n">{costCell(it.costFils, it.costFromRecipe)}</td>
                        <td class="n">{margin(it.priceFils, it.costFils)}</td>
                        <td class="from">
                          {it.costFromRecipe ? "Recipe" : it.costFils != null ? "Typed" : <span class="muted">None</span>}
                        </td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table></div>
            </section>
          )}
        </For>

        <section class="panel">
          <h2>Options</h2>
          <p class="meta">How an option changes its item: extra sugar adds some, no sugar takes it away.</p>
          <div class="table-wrap"><table class="table recipe-table">
            <thead>
              <tr>
                <th scope="col">Option</th>
                <th scope="col" class="n">Price change</th>
                <th scope="col" class="n">Cost</th>
                <th scope="col">From</th>
              </tr>
            </thead>
            <tbody>
              <For each={groups()}>
                {(g) => (
                  <For each={g.modifiers}>
                    {(m) => (
                      <tr classList={{ on: isEditing("option", m.id!) }} onClick={() => open({ kind: "option", id: m.id! })}>
                        <th scope="row">
                          <button type="button" class="link-row" onClick={(e) => {
                            e.stopPropagation();
                            open({ kind: "option", id: m.id! });
                          }}>{m.name}</button>
                          <span class="meta"> {g.name}</span>
                        </th>
                        <td class="n">{m.priceDeltaFils ? formatFils(m.priceDeltaFils) : ""}</td>
                        <td class="n">{costCell(m.costFils, m.costFromRecipe)}</td>
                        <td class="from">
                          {m.costFromRecipe ? "Recipe" : m.costFils != null ? "Typed" : <span class="muted">None</span>}
                        </td>
                      </tr>
                    )}
                  </For>
                )}
              </For>
            </tbody>
          </table></div>
        </section>
      </section>

      <aside class="editor recipe-card" classList={{ closed: editing() === undefined }} aria-label="Recipe">
        <Show when={editing()} fallback={
          <p class="editor-empty">Choose an item, option or batch to write down what goes into it.</p>
        }>
          <EditorHead
            title={editedItem()?.name ?? editedOption()?.name ?? (editing()?.kind === "batch" ? (bd.name || "New batch") : "")}
            onClose={() => setEditing(undefined)} />
          <FormError error={error()} fields={["name", "yieldPortions", "lines(\\.\\d+(\\.\\w+)?)?"]} />
          <Show when={editedItem()}>
            {(it) => <p class="meta">Sells at {formatFils(it().priceFils)}.</p>}
          </Show>
          <Show when={editedOption()}>
            {(m) => (
              <p class="meta">
                An option of {m().group}. Its lines change the item's recipe: below 0 takes away, as No sugar is -10 g of sugar.
              </p>
            )}
          </Show>
          <Show when={editing()?.kind === "batch"}>
            <Field label="Name" value={bd.name} placeholder="Chicken curry base" onInput={(v) => setBd("name", v)}
              error={fieldError(error(), "name")} />
            <Field label="Portions one batch makes" value={bd.portions} inputMode="decimal" placeholder="40"
              onInput={(v) => setBd("portions", v)} error={fieldError(error(), "yieldPortions")}
              hint="As the chef serves it: plates of curry, or of rice." />
            <Show when={editing()?.id}>
              <div class="field">
                <span>Meals that use it</span>
                <Show when={usersOf(editing()!.id!).length} fallback={
                  <span class="meta">None yet. Add it to a meal's recipe as a line: "Chicken curry base, 1 portion".</span>
                }>
                  <div class="used-by">
                    <For each={usersOf(editing()!.id!)}>
                      {(u) => (
                        <button type="button" class="btn btn-option btn-small" onClick={() => open({ kind: u.kind, id: u.id })}>
                          {u.name}
                        </button>
                      )}
                    </For>
                  </div>
                </Show>
              </div>
            </Show>
          </Show>

          <div class="field">
            <span>{editing()?.kind === "batch" ? "What goes in the pot" : "What goes into it"}</span>
            <Index each={lines.list}>
              {(l, i) => (
                <div class="recipe-line">
                  <select class="input" value={l().ref} onChange={(e) => setLines("list", i, "ref", e.currentTarget.value)}
                    aria-label="Ingredient or batch">
                    {/* Each option says whether it is chosen: the batches appear a
                        moment after the select's value is set, which then falls back. */}
                    <option value="" selected={!l().ref}>Choose…</option>
                    <Show when={editing()?.kind !== "batch" && batches().length}>
                      <optgroup label="Batches">
                        <For each={batches()}>
                          {(b) => <option value={`b:${b.id}`} selected={l().ref === `b:${b.id}`}>{b.name}</option>}
                        </For>
                      </optgroup>
                    </Show>
                    <optgroup label="Ingredients">
                      <For each={ingredients()}>
                        {(x) => <option value={`i:${x.id}`} selected={l().ref === `i:${x.id}`}>{x.name}</option>}
                      </For>
                    </optgroup>
                  </select>
                  <div class="recipe-amount">
                    <input class="input" inputmode="decimal" value={l().amount} aria-label="How much"
                      onInput={(e) => setLines("list", i, "amount", e.currentTarget.value)}
                      aria-invalid={fieldError(error(), `lines.${i}.amount`) ? "true" : undefined} />
                    <span class="unit">{unitOfRef(l().ref, l().amount)}</span>
                    <span class="line-cost num">
                      <Show when={lineCost(l()).fils !== null} fallback={<span class="error">No price</span>}>
                        {l().ref && l().amount ? formatFils(Math.round(lineCost(l()).fils!)) : ""}
                      </Show>
                    </span>
                    <button type="button" class="btn btn-danger btn-small" aria-label="Remove line"
                      onClick={() => setLines("list", (ls) => (ls.length > 1 ? ls.filter((_, j) => j !== i) : [{ ref: "", amount: "" }]))}>
                      Remove
                    </button>
                  </div>
                  <Show when={l().ref.startsWith("b:") && batch(l().ref.slice(2))}>
                    {(b) => (
                      <p class="batch-note">
                        From the batch: {formatFils(Math.round(b().portionCostFils ?? 0))} a portion, {grouped.format(b().yieldPortions)} to a
                        pot.{" "}
                        <button type="button" class="link-row" onClick={() => open({ kind: "batch", id: b().id })}>
                          Open the batch
                        </button>
                      </p>
                    )}
                  </Show>
                  <Show when={fieldError(error(), `lines.${i}.amount`) || fieldError(error(), `lines.${i}`)}>
                    <span class="error">{fieldError(error(), `lines.${i}.amount`) || fieldError(error(), `lines.${i}`)}</span>
                  </Show>
                </div>
              )}
            </Index>
            <button type="button" class="btn btn-option" onClick={() => setLines("list", (ls) => [...ls, { ref: "", amount: "" }])}>
              Add a line
            </button>
            <Show when={!ingredients().length}>
              <span class="meta">No ingredients yet: sort what you buy on <A href="/ingredients">Ingredients</A>.</span>
            </Show>
          </div>

          <div class="recipe-total">
            <Show when={!total().missing.length} fallback={
              <p><strong>No cost yet.</strong> {total().missing.join(", ")} {total().missing.length === 1 ? "has" : "have"} no price: buy {total().missing.length === 1 ? "it" : "them"} with a pack size on Purchases.</p>
            }>
              <Show when={editedItem()}>
                {(it) => (
                  <p>
                    <strong>Costs {formatFils(total().fils)}.</strong>{" "}
                    Makes {formatFils(it().priceFils - total().fils)} on each,{" "}
                    {it().priceFils > 0 ? `${Math.round(((it().priceFils - total().fils) / it().priceFils) * 100)}% of the price` : ""}.
                  </p>
                )}
              </Show>
              <Show when={editedOption()}>
                <p>
                  <strong>{total().fils < 0 ? `Takes ${formatFils(-total().fils)} off` : `Adds ${formatFils(total().fils)} to`}</strong> the
                  item's cost.
                </p>
              </Show>
              <Show when={editing()?.kind === "batch"}>
                <p>
                  <strong>The batch costs {formatAmount(total().fils)}</strong>
                  <Show when={parseAmountOf(bd.portions, false)}>
                    {(n) => <>, {formatFils(Math.round(total().fils / n()))} a portion</>}
                  </Show>
                  .
                </p>
              </Show>
            </Show>
            <p class="meta">At today's prices: each ingredient's average of its last 3 buys.</p>
          </div>

          <div class="row">
            <button type="button" class="btn btn-ink" disabled={busy()} onClick={save}>
              {editing()?.kind === "batch" ? (editing()?.id ? "Save batch" : "Add batch") : "Save recipe"}
            </button>
            <span class="spacer" />
            <Show when={(editing()?.kind === "item" && itemRecipe(editing()!.id!)) || (editing()?.kind === "option" && modRecipe(editing()!.id!))}>
              <DeleteButton label="Remove recipe" confirm="Tap again: back to a typed cost" disabled={busy()} onDelete={removeRecipe} />
            </Show>
            <Show when={editing()?.kind === "batch" && editing()?.id ? batch(editing()!.id!) : undefined}>
              {(b) => (
                <Show when={b().usedBy === 0} fallback={<span class="meta">Used in {b().usedBy} recipes</span>}>
                  <DeleteButton label="Delete batch" confirm="Tap again to delete" disabled={busy()} onDelete={() => removeBatch(b().id)} />
                </Show>
              )}
            </Show>
          </div>
          <p class="status" role="status">{status()}</p>
        </Show>
      </aside>
    </div>
  );
}

