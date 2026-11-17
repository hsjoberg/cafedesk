import { createSignal, For, Show } from "solid-js";
import { createStore } from "solid-js/store";
import { api, call, type Shop } from "../api/client";
import { currencyName } from "../money";
import { me, setMe } from "../state";
import { EditorHead, Field, fieldError, FormError, toSaveError, type SaveError } from "../ui";

type Draft = {
  name: string;
  timezone: string;
  tradeName: string;
  tradeNameAr: string;
  address: string;
  addressAr: string;
  phone: string;
  licenceNumber: string;
};

const fields = ["name", "timezone", "tradeName", "tradeNameAr", "address", "addressAr", "phone", "licenceNumber"];

type BusinessDraft = { name: string; currency: string; country: "AE" | ""; vatPercent: string };

const businessFields = ["name", "currency", "country", "vatPercent"];

/** The UAE's rules apply: receipts must show the trade name and address in Arabic. */
const inUAE = () => me()?.organization.country === "AE";

/** What a receipt still lacks of what it needs, by name: in the UAE, Arabic too. */
function missing(s: Draft | Shop): string[] {
  const out: string[] = [];
  if (!s.tradeName.trim()) out.push("the trade name");
  if (inUAE() && !s.tradeNameAr.trim()) out.push("the trade name in Arabic");
  if (!s.address.trim()) out.push("the address");
  if (inUAE() && !s.addressAr.trim()) out.push("the address in Arabic");
  return out;
}

const list = (parts: string[]) =>
  parts.length < 2 ? parts.join("") : `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;

/** The time zones the browser knows, for the shop's time zone. */
const timeZones = (() => {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return [];
  }
})();


/** The business's settings, the shops, and what their receipts print at the top. */
export function ShopPage() {
  const shops = () => me()?.shops ?? [];
  const org = () => me()?.organization;
  // A shop's ID, "business" for the business's settings, or nothing.
  const [editing, setEditing] = createSignal<string>();
  const [d, setD] = createStore<Draft>({ name: "", timezone: "", tradeName: "", tradeNameAr: "", address: "", addressAr: "",
    phone: "", licenceNumber: "" });
  const [b, setB] = createStore<BusinessDraft>({ name: "", currency: "", country: "", vatPercent: "" });
  const [error, setError] = createSignal<SaveError>();
  const [status, setStatus] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const open = (sh: Shop) => {
    setError(undefined);
    setStatus("");
    setD({
      name: sh.name,
      timezone: sh.timezone,
      tradeName: sh.tradeName,
      tradeNameAr: sh.tradeNameAr,
      address: sh.address,
      addressAr: sh.addressAr,
      phone: sh.phone,
      licenceNumber: sh.licenceNumber,
    });
    setEditing(sh.id);
  };
  const openBusiness = () => {
    const o = org();
    if (!o) return;
    setError(undefined);
    setStatus("");
    setB({ name: o.name, currency: o.currency, country: o.country, vatPercent: String(o.vatPercent) });
    setEditing("business");
  };
  // With one shop, its details are the page.
  const only = shops();
  if (only.length === 1 && only[0]) open(only[0]);

  const save = async () => {
    const id = editing();
    if (!id || id === "business") return;
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

  const saveBusiness = async () => {
    const vat = Number(b.vatPercent.trim() === "" ? "0" : b.vatPercent.replace(",", "."));
    if (!Number.isFinite(vat)) {
      setError({ message: "The VAT is a percentage, such as 5.", field: "vatPercent" });
      return;
    }
    setBusy(true);
    setError(undefined);
    setStatus("");
    try {
      const saved = await call(
        api.PUT("/v1/admin/organization", { body: { name: b.name, currency: b.currency, country: b.country, vatPercent: vat } }),
      );
      const m = me();
      if (m) setMe({ ...m, organization: saved });
      openBusiness();
      setStatus("Saved. The tills count in the currency from their next sync.");
    } catch (e) {
      setError(toSaveError(e));
    } finally {
      setBusy(false);
    }
  };

  const describe = (sh: Shop) => {
    const left = missing(sh);
    return left.length ? `Receipts lack ${list(left)}.` : inUAE() ? "Receipts have what the law asks for." : "Receipts are set up.";
  };

  return (
    <div class="workspace">
      <section class="wall" aria-label="Shop">
        <div class="wall-head">
          <h1>{shops().length === 1 ? "Shop" : "Shops"}</h1>
        </div>
        <p class="muted lede">
          <Show
            when={inUAE()}
            fallback={<>What the receipts print at the top: the business's name and address, in English, and in Arabic if you fill it in.</>}
          >
            What the receipts print at the top. The UAE's consumer protection law asks a receipt to show the business's
            trade name and address, in Arabic; English can stand beside it. Items print with their Arabic names from the
            menu.
          </Show>
        </p>
        <h2 class="subhead">Business</h2>
        <div class="list">
          <button type="button" aria-pressed={editing() === "business"} onClick={openBusiness}>
            <strong>{org()?.name}</strong>
            <span class="meta">
              {org()?.currency} ({currencyName(org()?.currency ?? "")}). {inUAE() ? "Follows the UAE's rules." : "No country's rules."}{" "}
              {org()?.vatPercent ? `Purchases can carry ${org()?.vatPercent}% VAT.` : "No VAT on purchases."}
            </span>
          </button>
        </div>
        <h2 class="subhead">{shops().length === 1 ? "The shop" : "The shops"}</h2>
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
        <Show when={editing() && editing() !== "business"}>
          <h2 class="subhead">The top of a receipt</h2>
          <div class="receipt-head" aria-label="How the top of a receipt prints">
            <strong classList={{ unset: !d.tradeName }}>{d.tradeName || "Trade name"}</strong>
            <Show when={inUAE() || d.tradeNameAr}>
              <strong lang="ar" dir="rtl" classList={{ unset: !d.tradeNameAr }}>{d.tradeNameAr || "الاسم التجاري"}</strong>
            </Show>
            <span classList={{ unset: !d.address }}>{d.address || "Address"}</span>
            <Show when={inUAE() || d.addressAr}>
              <span lang="ar" dir="rtl" classList={{ unset: !d.addressAr }}>{d.addressAr || "العنوان"}</span>
            </Show>
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
        <Show when={editing()} fallback={<p class="editor-empty">Choose the business or a shop to change its details.</p>}>
          <Show when={editing() === "business"}>
            <EditorHead title="Business" onClose={() => setEditing(undefined)} />
            <FormError error={error()} fields={businessFields} />
            <Field label="Name" value={b.name} onInput={(v) => setB("name", v)} error={fieldError(error(), "name")}
              hint="Your company, as the admin shows it." />
            <label class="field">
              <span>Currency</span>
              <select class="input" value={b.currency} onChange={(e) => setB("currency", e.currentTarget.value)}
                aria-invalid={fieldError(error(), "currency") ? "true" : undefined}>
                <For each={me()?.currencies ?? []}>
                  {(c) => (
                    <option value={c.code} selected={b.currency === c.code}>
                      {c.code}, {currencyName(c.code)}
                    </option>
                  )}
                </For>
              </select>
              <Show when={fieldError(error(), "currency")} fallback={<span class="meta">Every amount is in it, in every shop. It can change only until there are prices, sales or purchases.</span>}>
                <span class="error">{fieldError(error(), "currency")}</span>
              </Show>
            </label>
            <label class="field">
              <span>Country's rules</span>
              <select class="input" value={b.country} onChange={(e) => setB("country", e.currentTarget.value as "AE" | "")}>
                <option value="AE" selected={b.country === "AE"}>The UAE</option>
                <option value="" selected={b.country === ""}>None</option>
              </select>
              <span class="meta">
                With the UAE's, Sales warns as sales near the VAT registration threshold, and receipts ask for the
                business's name and address in Arabic too.
              </span>
            </label>
            <Field label="VAT on purchases, %" value={b.vatPercent} inputMode="decimal" onInput={(v) => setB("vatPercent", v)}
              error={fieldError(error(), "vatPercent")}
              hint="What suppliers add to prices before VAT, such as 5 in the UAE. 0 if they don't." />
            <div class="row">
              <button type="button" class="btn btn-ink" disabled={busy()} onClick={saveBusiness}>
                Save business
              </button>
            </div>
            <p class="status" role="status">{status()}</p>
          </Show>
          <Show when={editing() !== "business"}>
            <EditorHead title="Shop details" onClose={() => setEditing(undefined)} />
            <FormError error={error()} fields={fields} />
            <Field label="Name in the admin" value={d.name} onInput={(v) => setD("name", v)} error={fieldError(error(), "name")}
              hint="For you and the tills, such as the area. Receipts print the trade name instead." />
            <label class="field">
              <span>Time zone</span>
              <input class="input" list="time-zones" value={d.timezone} onInput={(e) => setD("timezone", e.currentTarget.value)}
                aria-invalid={fieldError(error(), "timezone") ? "true" : undefined} />
              <datalist id="time-zones">
                <For each={timeZones}>{(z) => <option value={z} />}</For>
              </datalist>
              <Show when={fieldError(error(), "timezone")} fallback={<span class="meta">Sales by hour count in it, such as Asia/Dubai.</span>}>
                <span class="error">{fieldError(error(), "timezone")}</span>
              </Show>
            </label>
            <Field label="Trade name" value={d.tradeName} onInput={(v) => setD("tradeName", v)}
              error={fieldError(error(), "tradeName")} hint="Exactly as on the trade licence." />
            <Field label="Trade name in Arabic" value={d.tradeNameAr} rtl onInput={(v) => setD("tradeNameAr", v)}
              error={fieldError(error(), "tradeNameAr")} hint={inUAE() ? undefined : "Optional."} />
            <Field label="Address" value={d.address} onInput={(v) => setD("address", v)}
              error={fieldError(error(), "address")} placeholder="Shop 3, Downtown, Dubai" />
            <Field label="Address in Arabic" value={d.addressAr} rtl onInput={(v) => setD("addressAr", v)}
              error={fieldError(error(), "addressAr")} hint={inUAE() ? undefined : "Optional."} />
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
        </Show>
      </aside>
    </div>
  );
}
