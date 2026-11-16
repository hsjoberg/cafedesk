import { A } from "@solidjs/router";
import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { api, call, type Terminal, type TerminalPairing } from "../api/client";
import { me } from "../state";
import { DeleteButton, EditorHead, Field, toSaveError, type SaveError } from "../ui";

const when = (iso?: string) =>
  iso ? new Date(iso).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) : "";

/** Tills: add one, pair it with a code, unpair it. */
export function TillsPage() {
  const [list, setList] = createSignal<Terminal[]>();
  const [loadError, setLoadError] = createSignal("");
  const load = async () => {
    try {
      setList(await call(api.GET("/v1/admin/terminals")));
      setLoadError("");
    } catch (e) {
      setLoadError((e as Error).message);
    }
  };
  onMount(load);

  const shops = () => me()?.shops ?? [];
  const shopName = (id: string) => shops().find((s) => s.id === id)?.name ?? "";
  const [editing, setEditing] = createSignal<string | null | undefined>(undefined); // null: new
  const [shopId, setShopId] = createSignal("");
  const [name, setName] = createSignal("");
  const [pairing, setPairingRaw] = createSignal<TerminalPairing>();
  // When the code was shown, the till's "connected at" as the server had it:
  // a new value means the till used the code. No clocks are compared.
  let connectedBefore: string | undefined;
  const [justConnected, setJustConnected] = createSignal<string>();
  const setPairing = (p: TerminalPairing | undefined) => {
    connectedBefore = p?.terminal.pairedAt;
    setJustConnected(undefined);
    setPairingRaw(p);
  };
  const [error, setError] = createSignal<SaveError>();
  const [busy, setBusy] = createSignal(false);

  // A clock for the code's time left.
  const [now, setNow] = createSignal(Date.now());
  const tick = setInterval(() => setNow(Date.now()), 1000);
  onCleanup(() => clearInterval(tick));

  // While a code is shown, look every few seconds for the till using it.
  const watch = setInterval(async () => {
    const p = pairing();
    if (!p || left() === 0 || busy()) return;
    await load();
    const t = list()?.find((x) => x.id === p.terminal.id);
    if (t && t.status === "paired" && t.pairedAt !== connectedBefore) {
      setPairingRaw(undefined);
      setJustConnected(t.id);
    }
  }, 3000);
  onCleanup(() => clearInterval(watch));
  const left = () => {
    const p = pairing();
    if (!p) return 0;
    return Math.max(0, Math.round((Date.parse(p.expiresAt) - now()) / 1000));
  };

  const selected = () => list()?.find((t) => t.id === editing());
  const open = (t: Terminal | null) => {
    setError(undefined);
    setPairing(undefined);
    setShopId(shops()[0]?.id ?? "");
    setName("");
    setEditing(t?.id ?? null);
  };

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(undefined);
    try {
      await fn();
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };
  const add = () =>
    run(async () => {
      const p = await call(api.POST("/v1/admin/terminals", { body: { shopId: shopId(), name: name() || undefined } }));
      await load();
      setEditing(p.terminal.id);
      setPairing(p);
    });
  const newCode = (id: string) =>
    run(async () => {
      setPairing(await call(api.POST("/v1/admin/terminals/{id}/pairing-code", { params: { path: { id } } })));
    });
  const unpair = (id: string) =>
    run(async () => {
      await call(api.DELETE("/v1/admin/terminals/{id}", { params: { path: { id } } }));
      setPairing(undefined);
      await load();
    });

  const describe = (t: Terminal) => {
    switch (t.status) {
      case "waiting":
        return "Waiting to be connected.";
      case "unpaired":
        return "Disconnected. Its sales wait on it until it is connected again.";
      default:
        return t.lastSeenAt ? `Connected. Last in touch ${when(t.lastSeenAt)}.` : "Connected.";
    }
  };
  const label = (t: Terminal) => (t.name ? `${t.code}, ${t.name}` : t.code);

  return (
    <div class="workspace">
      <section class="wall" aria-label="Tills">
        <div class="wall-head">
          <h1>Tills</h1>
          <span class="spacer" />
          <button type="button" class="btn btn-ink" onClick={() => open(null)}>
            Add a till
          </button>
        </div>
        <p class="muted lede">
          A till is connected once, with a code from here. It then gets the menu and staff, and sends its sales, whenever it
          is online. Its order numbers start with its name, as T1-012.
        </p>
        <Show when={loadError()}>
          <p class="banner" role="alert">{loadError()}</p>
        </Show>
        <Show when={list()?.length} fallback={<Show when={list()}><p class="empty">No tills yet. Add one, then type its code on the till.</p></Show>}>
          <div class="list">
            <For each={list()}>
              {(t) => (
                <button type="button" aria-pressed={editing() === t.id} onClick={() => open(t)}>
                  <strong classList={{ muted: t.status === "unpaired" }}>{label(t)}</strong>
                  <span class="meta">
                    <Show when={shops().length > 1}>{shopName(t.shopId)}. </Show>
                    {describe(t)}
                  </span>
                </button>
              )}
            </For>
          </div>
        </Show>
      </section>

      <aside class="editor" classList={{ closed: editing() === undefined }} aria-label="Till">
        <Show when={editing() !== undefined} fallback={<p class="editor-empty">Choose a till, or add one.</p>}>
          <Show when={editing() === null} fallback={
            <Show when={selected()}>
              {(t) => (
                <>
                  <EditorHead title={label(t())} onClose={() => setEditing(undefined)} />
                  <p class="muted">{describe(t())}</p>
                  <Show when={justConnected() === t().id}>
                    <div class="code-panel done" role="status">
                      <strong>The till is connected.</strong>
                      <p class="meta">
                        It is getting the menu and staff, then shows its sign-in screen. Sign in with a starting PIN from Staff.
                      </p>
                    </div>
                  </Show>
                  <Show when={pairing() && left() > 0}>
                    <div class="code-panel">
                      <span class="meta">Code for the till</span>
                      <p class="code num" aria-label={pairing()!.code.split("").join(" ")}>
                        {pairing()!.code.slice(0, 4)} {pairing()!.code.slice(4)}
                      </p>
                      <p class="meta">
                        Type it on the till's Connect screen, or in its Settings, under Admin connection, Connect again. It
                        works once, for{" "}
                        {Math.floor(left() / 60)}:{String(left() % 60).padStart(2, "0")} more.
                      </p>
                      <p class="meta waiting">Waiting for the till. This page updates when it connects.</p>
                    </div>
                  </Show>
                  <Show when={pairing() && left() === 0}>
                    <p class="banner">The code ran out. Show a new one.</p>
                  </Show>
                  <Show when={error()}>
                    <p class="banner" role="alert">{error()!.message}</p>
                  </Show>
                  <div class="row">
                    <button type="button" class="btn btn-ink" disabled={busy()} onClick={() => newCode(t().id)}>
                      {t().status === "paired" ? "Connect a new PC" : "Show a code"}
                    </button>
                    <span class="spacer" />
                    <Show when={t().status === "paired"}>
                      <DeleteButton label="Disconnect" confirm="Tap again to disconnect" disabled={busy()} onDelete={() => unpair(t().id)} />
                    </Show>
                  </div>
                  <Show when={t().status === "paired"}>
                    <p class="meta">
                      Connecting a new PC, as when the till's PC is replaced, disconnects this one. Disconnect a till that is
                      lost or stolen: it can no longer reach the admin.
                    </p>
                  </Show>
                </>
              )}
            </Show>
          }>
            <EditorHead title="New till" onClose={() => setEditing(undefined)} />
            <Show when={shops().length > 1}>
              <label class="field">
                <span>Shop</span>
                <select class="input" value={shopId()} onChange={(e) => setShopId(e.currentTarget.value)}>
                  <For each={shops()}>{(s) => <option value={s.id}>{s.name}</option>}</For>
                </select>
              </label>
            </Show>
            <Field label="Where it stands (optional)" value={name()} placeholder="Front counter" onInput={setName} />
            <Show when={error()}>
              <p class="banner" role="alert">
                {error()!.message}{" "}
                <Show when={error()!.message.includes("Staff page")}>
                  <A href="/staff">Open Staff</A>
                </Show>
              </p>
            </Show>
            <div class="row">
              <button type="button" class="btn btn-ink" disabled={busy()} onClick={add}>
                Add till and show its code
              </button>
            </div>
          </Show>
        </Show>
      </aside>
    </div>
  );
}
