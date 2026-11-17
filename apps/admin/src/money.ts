import { me } from "./state";

// Money is whole units of the currency's smallest part, as the API sends
// it: fils for dirhams, cents for dollars, baisa for the Omani rial. The
// business's currency says how many decimals it has.

/** The business's currency code, such as "AED". */
export const currencyCode = () => me()?.organization.currency ?? "AED";

/** How many decimals the business's currency has: 2 for dirhams, 3 for dinars, 0 for yen. */
export const decimals = () => me()?.organization.currencyDecimals ?? 2;

/** How many of the smallest unit make one: 100 for two decimals. */
export const unit = () => 10 ** decimals();

/** The currency's name in English, such as "UAE Dirham". */
export function currencyName(code: string): string {
  try {
    return new Intl.DisplayNames(["en"], { type: "currency" }).of(code) ?? code;
  } catch {
    return code;
  }
}

/** Formats an amount as "16.50", with the currency's decimals. */
export function formatFils(fils: number): string {
  const d = decimals();
  const sign = fils < 0 ? "-" : "";
  const abs = Math.abs(fils);
  const u = unit();
  const whole = Math.floor(abs / u);
  return d === 0 ? `${sign}${whole}` : `${sign}${whole}.${String(abs % u).padStart(d, "0")}`;
}

const formats = new Map<number, Intl.NumberFormat>();

/** Formats an amount with thousands grouped, as "1,234.50". */
export function formatAmount(fils: number): string {
  const d = decimals();
  let f = formats.get(d);
  if (!f) {
    f = new Intl.NumberFormat("en", { minimumFractionDigits: d, maximumFractionDigits: d });
    formats.set(d, f);
  }
  return f.format(fils / unit());
}

/** Formats an amount with the currency's code, as "AED 1,234.50". */
export const withCode = (fils: number) => `${currencyCode()} ${formatAmount(fils)}`;

/** Reads a quantity, as "2", "1.5" or "0.250": more than 0, 3 decimals at most. */
export function parseQuantity(text: string): number | null {
  const m = /^\s*(\d{0,7})(?:\.(\d{0,3}))?\s*$/.exec(text);
  if (!m || (m[1] === "" && (m[2] ?? "") === "")) return null;
  const q = Number(`${m[1] || "0"}.${m[2] || "0"}`);
  return q > 0 ? q : null;
}

/** Formats a quantity without trailing zeros: 2, 1.5. */
export function formatQuantity(q: number): string {
  return String(Math.round(q * 1000) / 1000);
}

/**
 * Reads an amount typed in whole units of the currency, as "2", "2.5" or
 * "2.50" in dirhams, into its smallest unit. Returns null for anything
 * else, or more decimals than the currency has. A leading minus is
 * allowed when signed.
 */
export function parseAmount(text: string, signed = false): number | null {
  const d = decimals();
  const m = /^\s*(-?)(\d{0,9})(?:\.(\d*))?\s*$/.exec(text);
  if (!m || (m[2] === "" && (m[3] ?? "") === "") || (m[1] && !signed)) return null;
  const frac = m[3] ?? "";
  if (frac.length > d) return null;
  const whole = Number(m[2] || "0");
  const fils = whole * unit() + Number(frac.padEnd(d, "0") || "0");
  return m[1] ? -fils : fils;
}
