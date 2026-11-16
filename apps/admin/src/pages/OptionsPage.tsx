import { createSignal, For, Index, onMount, Show } from "solid-js";
import { createStore, produce } from "solid-js/store";
import { api, call, type ModifierGroup } from "../api/client";
import { formatFils, parseAmount } from "../money";
import { catalog, catalogError, reloadCatalog } from "../state";
import { DeleteButton, EditorHead, Field, fieldError, FormError, toSaveError, type SaveError } from "../ui";

type ModDraft = { id?: string; name: string; nameAr: string; delta: string; isDefault: boolean };
type GroupDraft = { name: string; nameAr: string; multiple: boolean; modifiers: ModDraft[] };

const blankMod = (): ModDraft => ({ name: "", nameAr: "", delta: "0.00", isDefault: false });

/** Option groups: sugar levels, spice, extras. Items choose which they have. */
export function OptionsPage() {
  onMount(reloadCatalog);
  const groups = () => catalog()?.modifierGroups ?? [];
  const [editing, setEditing] = createSignal<string | null | undefined>(undefined); // undefined: none; null: new
  const [g, setG] = createStore<GroupDraft>({ name: "", nameAr: "", multiple: false, modifiers: [] });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const usedBy = (id: string) => (catalog()?.items ?? []).filter((i) => i.modifierGroupIds.includes(id)).length;
  const edited = () => groups().find((x) => x.id === editing());

  const open = (group: ModifierGroup | null) => {
    setError(undefined);
    setStatus("");
    setG(
      group
        ? { name: group.name, nameAr: group.nameAr, multiple: group.multiple,
            modifiers: group.modifiers.map((m) => ({ id: m.id, name: m.name, nameAr: m.nameAr ?? "",
              delta: formatFils(m.priceDeltaFils), isDefault: m.isDefault })) }
        : { name: "", nameAr: "", multiple: false, modifiers: [{ ...blankMod(), isDefault: true }, blankMod()] },
    );
    setEditing(group?.id ?? null);
  };

  const setDefault = (i: number, on: boolean) =>
    setG(produce((d) => {
      d.modifiers.forEach((m, j) => {
        if (j === i) m.isDefault = on;
        else if (!d.multiple && on) m.isDefault = false;
      });
    }));

  const save = async () => {
    setBusy(true);
    setError(undefined);
    setStatus("");
    try {
      const modifiers = g.modifiers.map((m, i) => {
        const delta = parseAmount(m.delta || "0", true);
        if (delta === null) throw { field: `modifiers.${i}.priceDeltaFils`, message: "Enter a price change like 1.00 or -0.50." };
        return { id: m.id, name: m.name, nameAr: m.nameAr, priceDeltaFils: delta, isDefault: m.isDefault };
      });
      const body = { name: g.name, nameAr: g.nameAr, multiple: g.multiple, modifiers };
      const id = editing();
      const saved = id
        ? await call(api.PUT("/v1/admin/modifier-groups/{id}", { params: { path: { id } }, body }))
        : await call(api.POST("/v1/admin/modifier-groups", { body }));
      await reloadCatalog();
      open(saved);
      setStatus("Saved.");
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    setBusy(true);
    try {
      await call(api.DELETE("/v1/admin/modifier-groups/{id}", { params: { path: { id } } }));
      await reloadCatalog();
      setEditing(undefined);
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const move = async (id: string, position: number) => {
    setBusy(true);
    try {
      await call(api.POST("/v1/admin/modifier-groups/{id}/move", { params: { path: { id } }, body: { position } }));
      await reloadCatalog();
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div class="workspace">
      <section class="wall" aria-label="Option groups">
        <Show when={catalogError()}>
          <p class="banner" role="alert">{catalogError()}</p>
        </Show>
        <div class="wall-head">
          <h1>Options</h1>
          <span class="spacer" />
          <button type="button" class="btn btn-ink" onClick={() => open(null)}>
            Add an option group
          </button>
        </div>
        <p class="muted" style={{ "margin-top": "0", "max-width": "60ch" }}>
          Groups of choices the cashier makes for an item, such as its sugar level. Give items their groups on the Menu page.
        </p>
        <Show when={groups().length > 0} fallback={
          <Show when={catalog()}>
            <p class="empty">No option groups yet. Add one, such as Sugar with Normal, Less and None.</p>
          </Show>
        }>
          <div class="list">
            <For each={groups()}>
              {(x) => (
                <button type="button" aria-pressed={editing() === x.id} onClick={() => open(x)}>
                  <strong>{editing() === x.id ? g.name || x.name : x.name}</strong>
                  <span class="meta">
                    {x.multiple ? "Any number can be chosen" : "One is chosen"}. {x.modifiers.map((m) => m.name).join(", ")}.
                    {" "}On {usedBy(x.id)} {usedBy(x.id) === 1 ? "item" : "items"}.
                  </span>
                </button>
              )}
            </For>
          </div>
        </Show>
      </section>

      <aside class="editor" classList={{ closed: editing() === undefined }} aria-label="Editor">
        <Show when={editing() !== undefined} fallback={
          <p class="editor-empty">Choose an option group to change it, or add one.</p>
        }>
          <EditorHead title={editing() ? "Edit option group" : "New option group"} onClose={() => setEditing(undefined)} />
          <FormError error={error()} fields={["name", "nameAr", "modifiers\\.\\d+\\.\\w+"]} />
          <Field label="Name" value={g.name} placeholder="Sugar" onInput={(v) => setG("name", v)}
            error={fieldError(error(), "name")} />
          <Field label="Arabic name" value={g.nameAr} rtl onInput={(v) => setG("nameAr", v)} error={fieldError(error(), "nameAr")} />
          <fieldset class="field" style={{ border: "0", padding: "0", margin: "0" }}>
            <span>How many can the cashier choose?</span>
            <label class="check">
              <input type="radio" name="multiple" checked={!g.multiple} onChange={() => {
                setG("multiple", false);
                const first = g.modifiers.findIndex((m) => m.isDefault);
                if (first >= 0) setDefault(first, true);
              }} />
              <span>One, such as a sugar level</span>
            </label>
            <label class="check">
              <input type="radio" name="multiple" checked={g.multiple} onChange={() => setG("multiple", true)} />
              <span>Any number, such as extras</span>
            </label>
          </fieldset>

          <div class="field">
            <span>Options</span>
            <Index each={g.modifiers}>
              {(m, i) => (
                <div class="mod">
                  <Field label="Name" value={m().name} onInput={(v) => setG("modifiers", i, "name", v)}
                    error={fieldError(error(), `modifiers.${i}.name`)} />
                  <div class="mod-row">
                    <Field label="Arabic name" value={m().nameAr} rtl onInput={(v) => setG("modifiers", i, "nameAr", v)} />
                    <Field label="Price change" value={m().delta} inputMode="decimal"
                      onInput={(v) => setG("modifiers", i, "delta", v)}
                      error={fieldError(error(), `modifiers.${i}.priceDeltaFils`)} />
                  </div>
                  <div class="row">
                    <label class="check">
                      <input type={g.multiple ? "checkbox" : "radio"} name="default" checked={m().isDefault}
                        onChange={(e) => setDefault(i, e.currentTarget.checked)} />
                      <span>Chosen when the item is tapped</span>
                    </label>
                    <span class="spacer" />
                    <button type="button" class="btn btn-danger btn-small" disabled={g.modifiers.length <= 1}
                      onClick={() => setG("modifiers", (list) => list.filter((_, j) => j !== i))}>
                      Remove
                    </button>
                  </div>
                </div>
              )}
            </Index>
            <button type="button" class="btn btn-option" onClick={() => setG("modifiers", (list) => [...list, blankMod()])}>
              Add an option
            </button>
          </div>

          <Show when={edited()}>
            {(x) => (
              <div class="row">
                <button type="button" class="btn btn-option btn-small" disabled={busy() || x().position === 0}
                  onClick={() => move(x().id, x().position - 1)}>Move earlier</button>
                <button type="button" class="btn btn-option btn-small" disabled={busy() || x().position >= groups().length - 1}
                  onClick={() => move(x().id, x().position + 1)}>Move later</button>
              </div>
            )}
          </Show>
          <div class="row">
            <button type="button" class="btn btn-ink" disabled={busy()} onClick={save}>
              {editing() ? "Save option group" : "Add option group"}
            </button>
            <span class="spacer" />
            <Show when={edited()}>
              {(x) => (
                <DeleteButton label="Delete group"
                  confirm={usedBy(x().id) ? `Delete, and take it off ${usedBy(x().id)} items` : "Tap again to delete"}
                  disabled={busy()} onDelete={() => remove(x().id)} />
              )}
            </Show>
          </div>
          <p class="status" role="status">{status()}</p>
        </Show>
      </aside>
    </div>
  );
}
