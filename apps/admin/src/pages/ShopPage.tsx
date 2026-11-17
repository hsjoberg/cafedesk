import { createSignal, For, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { api, call, type Shop } from "../api/client";
import { me, setMe } from "../state";
import { EditorHead, Field, fieldError, FormError, toSaveError, type SaveError } from "../ui";

type Draft = {
  name: string;
  tradeName: string;
  tradeNameAr: string;
  address: string;
  addressAr: string;
  phone: string;
  licenceNumber: string;
};

const fields = ["name", "tradeName", "tradeNameAr", "address", "addressAr", "phone", "licenceNumber"];

/** What a receipt still lacks of what the law asks for, by name. */
function missing(s: Draft): string[] {
  const out: string[] = [];
  if (!s.tradeName.trim()) out.push("the trade name");
  if (!s.tradeNameAr.trim()) out.push("the trade name in Arabic");
  if (!s.address.trim()) out.push("the address");
  if (!s.addressAr.trim()) out.push("the address in Arabic");
  return out;
}

const list = (parts: string[]) =>
  parts.length < 2 ? parts.join("") : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;

/** The shops, and what their receipts print at the top. */
export function ShopPage() {
  const shops = () => me()?.shops ?? [];
  const [editing, setEditing] = createSignal<string>();
  const [d, setD] = createStore<Draft>({ name: "", tradeName: "", tradeNameAr: "", address: "", addressAr: "", phone: "", licenceNumber: "" });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const open = (sh: Shop) => {
    setError(undefined);
    setStatus("");
    setD({
      name: sh.name,
      tradeName: sh.tradeName,
      tradeNameAr: sh.tradeNameAr,
      address: sh.address,
      addressAr: sh.addressAr,
      phone: sh.phone,
      licenceNumber: sh.licenceNumber,
    });
    setEditing(sh.id);
  };
  // With one shop, its details are the page.
  const only = shops();
  if (only.length === 1 && only[0]) open(only[0]);

  const save = async () => {
    const id = editing();
    if (!id) return;
    setBusy(true);
    setError(undefined);
    setStatus("");
    try {
      const saved = await call(api.PUT("/v1/admin/shops/{id}", { params: { path: { id } }, body: { ...d } }));
      const m = me();
      if (m) setMe({ ...m, shops: m.shops.map((s) => (s.id === saved.id ? saved : s)) });
      open(saved);
      const left = missing(saved);
      setStatus(
        left.length
          ? `Saved. The tills print it from their next sync. Receipts still lack ${list(left)}.`
          : "Saved. The tills print it from their next sync.",
      );
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const describe = (sh: Shop) => {
    const left = missing(sh);
    return left.length ? `Receipts lack ${list(left)}.` : "Receipts have what the law asks for.";
  };

  return (
    <div class="workspace">
      <section class="wall" aria-label="Shop">
        <div class="wall-head">
          <h1>{shops().length === 1 ? "Shop" : "Shops"}</h1>
        </div>
        <p class="muted lede">
          What the receipts print at the top. The UAE's consumer protection law asks a receipt to show the business's
          trade name and address, in Arabic; English can stand beside it. Items print with their Arabic names from the
          menu.
        </p>
        <div class="list">
          <For each={shops()}>
            {(sh) => (
              <button type="button" aria-pressed={editing() === sh.id} onClick={() => open(sh)}>
                <strong>{sh.name}</strong>
                <span class="meta">
                  <Show when={sh.tradeName}>{sh.tradeName}. </Show>
                  {describe(sh)}
                </span>
              </button>
            )}
          </For>
        </div>
        <Show when={editing()}>
          <h2 class="subhead">The top of a receipt</h2>
          <div class="receipt-head" aria-label="How the top of a receipt prints">
            <strong classList={{ unset: !d.tradeName }}>{d.tradeName || "Trade name"}</strong>
            <strong lang="ar" dir="rtl" classList={{ unset: !d.tradeNameAr }}>{d.tradeNameAr || "الاسم التجاري"}</strong>
            <span classList={{ unset: !d.address }}>{d.address || "Address"}</span>
            <span lang="ar" dir="rtl" classList={{ unset: !d.addressAr }}>{d.addressAr || "العنوان"}</span>
            <Show when={d.phone}>
              <span>Tel {d.phone}</span>
            </Show>
            <Show when={d.licenceNumber}>
              <span>Licence {d.licenceNumber}</span>
            </Show>
          </div>
        </Show>
      </section>

      <aside class="editor" classList={{ closed: !editing() }} aria-label="Editor">
        <Show when={editing()} fallback={<p class="editor-empty">Choose a shop to change its details.</p>}>
          <EditorHead title="Shop details" onClose={() => setEditing(undefined)} />
          <FormError error={error()} fields={fields} />
          <Field label="Name in the admin" value={d.name} onInput={(v) => setD("name", v)} error={fieldError(error(), "name")}
            hint="For you and the tills, such as the area. Receipts print the trade name instead." />
          <Field label="Trade name" value={d.tradeName} onInput={(v) => setD("tradeName", v)}
            error={fieldError(error(), "tradeName")} hint="Exactly as on the trade licence." />
          <Field label="Trade name in Arabic" value={d.tradeNameAr} rtl onInput={(v) => setD("tradeNameAr", v)}
            error={fieldError(error(), "tradeNameAr")} />
          <Field label="Address" value={d.address} onInput={(v) => setD("address", v)}
            error={fieldError(error(), "address")} placeholder="Shop 3, Downtown, Dubai" />
          <Field label="Address in Arabic" value={d.addressAr} rtl onInput={(v) => setD("addressAr", v)}
            error={fieldError(error(), "addressAr")} />
          <Field label="Phone" value={d.phone} onInput={(v) => setD("phone", v)} error={fieldError(error(), "phone")}
            hint="Optional. For customers to reach you." />
          <Field label="Trade licence number" value={d.licenceNumber} onInput={(v) => setD("licenceNumber", v)}
            error={fieldError(error(), "licenceNumber")} hint="Optional. Printed if set." />
          <div class="row">
            <button type="button" class="btn btn-ink" disabled={busy()} onClick={save}>
              Save shop details
            </button>
          </div>
          <p class="status" role="status">{status()}</p>
        </Show>
      </aside>
    </div>
  );
}
