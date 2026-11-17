import { createSignal, onMount, Show } from "solid-js";
import { api, call, type VatStatus } from "../api/client";
import { formatAmount } from "../money";

/** From this share of the mandatory threshold on, the notice shows. */
const approaching = 0.8;

/**
 * A notice when the last 12 months' sales near or pass the VAT registration
 * threshold (AED 375,000): a business above it must register with the
 * Federal Tax Authority, and missing it is fined. Nothing shows below 80%
 * of it, or once a shop is registered.
 */
export function VatNotice() {
  const [st, setSt] = createSignal<VatStatus>();
  onMount(async () => {
    try {
      setSt(await call(api.GET("/v1/admin/reports/vat")));
    } catch {
      // A warning only; the page works without it.
    }
  });
  const show = () => {
    const s = st();
    return s && !s.registered && s.salesFils >= s.mandatoryFils * approaching;
  };
  const over = () => st()!.salesFils >= st()!.mandatoryFils;
  return (
    <Show when={show()}>
      <div class="notice" classList={{ calm: !over() }} role="status">
        <p>
          <strong>
            {over() ? "Time to register for VAT. " : "Getting close to registering for VAT. "}
          </strong>
          Sales over the last 12 months come to AED {formatAmount(st()!.salesFils)}.{" "}
          {over()
            ? `Above AED ${formatAmount(st()!.mandatoryFils)}, a business must register with the Federal Tax Authority, within 30 days of passing it, and then charge VAT and print its TRN on receipts.`
            : `At AED ${formatAmount(st()!.mandatoryFils)}, registering with the Federal Tax Authority becomes required, within 30 days of passing it. It is allowed from AED ${formatAmount(st()!.voluntaryFils)}.`}
        </p>
      </div>
    </Show>
  );
}
