import { createSignal, For, onMount, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { api, call, type Staff } from "../api/client";
import { me } from "../state";
import { EditorHead, Field, fieldError, FormError, toSaveError, type SaveError } from "../ui";

type Draft = { name: string; role: "cashier" | "manager"; shopIds: string[]; active: boolean; pin: string };

/** Till staff: who signs in to the tills, with a PIN. */
export function StaffPage() {
  const [list, setList] = createSignal<Staff[]>();
  const [loadError, setLoadError] = createSignal("");
  const load = async () => {
    try {
      setList(await call(api.GET("/v1/admin/staff")));
      setLoadError("");
    } catch (e) {
      setLoadError((e as Error).message);
    }
  };
  onMount(load);

  const shops = () => me()?.shops ?? [];
  const shopName = (id: string) => shops().find((s) => s.id === id)?.name ?? "";
  const [editing, setEditing] = createSignal<string | null | undefined>(undefined); // null: new
  const [d, setD] = createStore<Draft>({ name: "", role: "cashier", shopIds: [], active: true, pin: "" });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const open = (st: Staff | null) => {
    setError(undefined);
    setStatus("");
    setD(st
      ? { name: st.name, role: st.role, shopIds: [...st.shopIds], active: st.active, pin: "" }
      : { name: "", role: "cashier", shopIds: shops().map((s) => s.id), active: true, pin: "" });
    setEditing(st?.id ?? null);
  };

  const save = async () => {
    setBusy(true);
    setError(undefined);
    setStatus("");
    try {
      const body = { name: d.name, role: d.role, shopIds: d.shopIds, active: d.active, pin: d.pin || undefined };
      const id = editing();
      const saved = id
        ? await call(api.PUT("/v1/admin/staff/{id}", { params: { path: { id } }, body }))
        : await call(api.POST("/v1/admin/staff", { body }));
      await load();
      open(saved);
      setStatus(body.pin ? `Saved. ${saved.name} chooses their own PIN at their next sign-in on a till.` : "Saved.");
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const describe = (st: Staff) => {
    if (!st.active) return "Turned off";
    return st.mustChangePin ? "Chooses their own PIN at their next sign-in" : "Signs in with their own PIN";
  };

  return (
    <div class="workspace">
      <section class="wall" aria-label="Staff">
        <div class="wall-head">
          <h1>Staff</h1>
          <span class="spacer" />
          <button type="button" class="btn btn-ink" onClick={() => open(null)}>
            Add a person
          </button>
        </div>
        <p class="muted lede">
          The people who sign in to the tills with a 4-digit PIN. Managers also approve voids, refunds and closing the day.
          Changes reach the tills when they next sync.
        </p>
        <Show when={loadError()}>
          <p class="banner" role="alert">{loadError()}</p>
        </Show>
        <Show when={list()?.length} fallback={
          <Show when={list()}>
            <p class="empty">No one yet. Add a manager first: a till can only be paired once its shop has one.</p>
          </Show>
        }>
          <div class="list">
            <For each={list()}>
              {(st) => (
                <button type="button" aria-pressed={editing() === st.id} onClick={() => open(st)}>
                  <strong classList={{ muted: !st.active }}>{editing() === st.id ? d.name || st.name : st.name}</strong>
                  <span class="meta">
                    {st.role === "manager" ? "Manager" : "Cashier"}
                    <Show when={shops().length > 1}>, {st.shopIds.map(shopName).join(" and ")}</Show>. {describe(st)}.
                  </span>
                </button>
              )}
            </For>
          </div>
        </Show>
      </section>

      <aside class="editor" classList={{ closed: editing() === undefined }} aria-label="Editor">
        <Show when={editing() !== undefined} fallback={<p class="editor-empty">Choose a person to change them, or add one.</p>}>
          <EditorHead title={editing() ? "Edit person" : "New person"} onClose={() => setEditing(undefined)} />
          <FormError error={error()} fields={["name", "role", "shopIds", "pin"]} />
          <Field label="Name" value={d.name} onInput={(v) => setD("name", v)} error={fieldError(error(), "name")} />
          <fieldset class="field bare">
            <span>Role</span>
            <label class="check">
              <input type="radio" name="role" checked={d.role === "cashier"} onChange={() => setD("role", "cashier")} />
              <span>Cashier <span class="meta">Rings up orders.</span></span>
            </label>
            <label class="check">
              <input type="radio" name="role" checked={d.role === "manager"} onChange={() => setD("role", "manager")} />
              <span>Manager <span class="meta">Also approves voids, refunds and closing the day.</span></span>
            </label>
          </fieldset>
          <Show when={shops().length > 1}>
            <fieldset class="field bare">
              <span>Shops whose tills they use</span>
              <For each={shops()}>
                {(sh) => (
                  <label class="check">
                    <input type="checkbox" checked={d.shopIds.includes(sh.id)} onChange={(e) =>
                      setD("shopIds", e.currentTarget.checked ? [...d.shopIds, sh.id] : d.shopIds.filter((x) => x !== sh.id))} />
                    <span>{sh.name}</span>
                  </label>
                )}
              </For>
              <Show when={fieldError(error(), "shopIds")}><span class="error">{fieldError(error(), "shopIds")}</span></Show>
            </fieldset>
          </Show>
          <label class="field">
            <span>{editing() ? "New starting PIN" : "Starting PIN"}</span>
            <input class="input pin" value={d.pin} inputmode="numeric" maxlength="4" autocomplete="off" placeholder="4 digits"
              aria-invalid={fieldError(error(), "pin") ? "true" : undefined}
              onInput={(e) => setD("pin", e.currentTarget.value.replace(/\D/g, "").slice(0, 4))} />
            <Show when={fieldError(error(), "pin")} fallback={
              <span class="meta">
                {editing()
                  ? "Leave it empty to keep their PIN. A new one is for someone who forgot theirs."
                  : "Tell them this PIN. They choose their own at their first sign-in."}
              </span>
            }>
              <span class="error">{fieldError(error(), "pin")}</span>
            </Show>
          </label>
          <label class="check">
            <input type="checkbox" checked={d.active} onChange={(e) => setD("active", e.currentTarget.checked)} />
            <span>Can sign in to tills</span>
          </label>
          <div class="row">
            <button type="button" class="btn btn-ink" disabled={busy()} onClick={save}>
              {editing() ? "Save person" : "Add person"}
            </button>
          </div>
          <p class="status" role="status">{status()}</p>
        </Show>
      </aside>
    </div>
  );
}
