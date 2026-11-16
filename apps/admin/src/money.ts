// Money is whole fils (1 AED = 100 fils), as the API sends it.

/** Formats fils as "16.50". */
export function formatFils(fils: number): string {
  const sign = fils < 0 ? "-" : "";
  const abs = Math.abs(fils);
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, "0")}`;
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
