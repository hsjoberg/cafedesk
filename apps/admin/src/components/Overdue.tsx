import { A } from "@solidjs/router";
import { createEffect, createSignal, For, on, Show } from "solid-js";
import { api, call, type Supplier } from "../api/client";
import { addDays, daysBetween, shortDate, todayIn } from "../dates";
import { me } from "../state";

/** Days past a supplier's usual gap before its invoice counts as missing. */
const grace = 2;

/**
 * A warning for each watched supplier whose invoice is overdue: none
 * recorded for longer than its usual gap, and then some. A missing
 * wholesale invoice makes every figure after it wrong, so it shows where the
 * owner looks. It can be quieted (no invoice this time, or remind me later)
 * or a supplier left unwatched; that is kept on the server, for everyone.
 */
export function OverdueInvoices(props: { onAdd?: (supplier: string) => void; refresh?: number }) {
  const [late, setLate] = createSignal<{ s: Supplier; days: number }[]>([]);
  const [busy, setBusy] = createSignal(false);
  const today = () => todayIn(me()?.shops[0]?.timezone ?? "Asia/Dubai");
  const check = async () => {
    try {
      const list = await call(api.GET("/v1/admin/suppliers"));
      const t = today();
      setLate(
        list
          .filter((s) => s.watched && s.usualDays && s.lastPurchasedOn && !(s.quietUntil && s.quietUntil >= t))
          .map((s) => ({ s, days: daysBetween(s.lastPurchasedOn!, t) - 1 }))
          .filter(({ s, days }) => days > s.usualDays! + grace),
      );
    } catch {
      // A warning only; the page works without it.
    }
  };
  // Again whenever the page says purchases changed.
  createEffect(on(() => props.refresh, check));

  const set = async (s: Supplier, watched: boolean, quietUntil: string | null) => {
    setBusy(true);
    try {
      await call(api.PUT("/v1/admin/suppliers/{id}", { params: { path: { id: s.id } }, body: { watched, quietUntil } }));
      await check();
    } catch {
      // Left as it was; the warning stays.
    } finally {
      setBusy(false);
    }
  };
  // No invoice this time: quiet until the one after would be overdue.
  const skip = (s: Supplier) => set(s, true, addDays(s.lastPurchasedOn!, 2 * s.usualDays! + grace));
  // Remind me in 2 days: quiet through tomorrow.
  const later = (s: Supplier) => set(s, true, addDays(today(), 1));
  const unwatch = (s: Supplier) => set(s, false, null);

  return (
    <For each={late()}>
      {({ s, days }) => (
        <div class="notice" role="alert">
          <p>
            <strong>No invoice from {s.name} since {shortDate(s.lastPurchasedOn!, true)}, {days} days ago.</strong>{" "}
            It usually comes every {s.usualDays} days. Until it's recorded, purchases and food cost are short.
          </p>
          <div class="notice-actions">
            <Show when={props.onAdd} fallback={<A href="/purchases" class="btn btn-ink btn-small">Record it on Purchases</A>}>
              <button type="button" class="btn btn-ink btn-small" onClick={() => props.onAdd!(s.name)}>
                Record it now
              </button>
            </Show>
            <button type="button" class="btn btn-option btn-small" disabled={busy()} onClick={() => skip(s)}>
              No invoice this time
            </button>
            <button type="button" class="btn btn-option btn-small" disabled={busy()} onClick={() => later(s)}>
              Remind me in 2 days
            </button>
            <button type="button" class="btn btn-plain btn-small" disabled={busy()} onClick={() => unwatch(s)}>
              Don't watch {s.name}
            </button>
          </div>
        </div>
      )}
    </For>
  );
}
