// Money is whole fils (1 AED = 100 fils), as the API sends it.

/** Formats fils as "16.50". */
export function formatFils(fils: number): string {
  const sign = fils < 0 ? "-" : "";
  const abs = Math.abs(fils);
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, "0")}`;
}

const grouped = new Intl.NumberFormat("en", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

/** Formats fils with thousands grouped, as "1,234.50". */
export function formatAmount(fils: number): string {
  return grouped.format(fils / 100);
}

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
 * Reads an amount typed in dirhams, as "2", "2.5" or "2.50", into fils.
 * Returns null for anything else. A leading minus is allowed when signed.
 */
export function parseAmount(text: string, signed = false): number | null {
  const m = /^\s*(-?)(\d{0,7})(?:\.(\d{0,2}))?\s*$/.exec(text);
  if (!m || (m[2] === "" && (m[3] ?? "") === "") || (m[1] && !signed)) return null;
  const whole = Number(m[2] || "0");
  const frac = Number((m[3] ?? "").padEnd(2, "0") || "0");
  const fils = whole * 100 + frac;
  return m[1] ? -fils : fils;
}
