import type { Ingredient, Unit } from "./api/client";

// Reading what a product name says: "Basmati rice, 10 kg" is basmati rice,
// 10,000 g. Only ever a suggestion, shown filled in for a person to check.

/**
 * Reads a pack size as typed off a label: "8160", "48 x 170", "12 × 400",
 * "2*6*250". Returns null for anything else.
 */
export function parsePack(text: string): number | null {
  const parts = text.split(/[x×*]/i).map((p) => p.trim().replace(/,/g, ""));
  if (parts.some((p) => !/^\d{1,7}(\.\d{1,3})?$/.test(p))) return null;
  const n = parts.reduce((a, p) => a * Number(p), 1);
  return n > 0 && n <= 10_000_000 ? Math.round(n * 1000) / 1000 : null;
}

const scale: Record<string, { unit: Unit; by: number }> = {
  kg: { unit: "g", by: 1000 },
  g: { unit: "g", by: 1 },
  gm: { unit: "g", by: 1 },
  gms: { unit: "g", by: 1 },
  l: { unit: "ml", by: 1000 },
  ltr: { unit: "ml", by: 1000 },
  litre: { unit: "ml", by: 1000 },
  liter: { unit: "ml", by: 1000 },
  ml: { unit: "ml", by: 1 },
};
// A size: one or more factors and a unit, as "35kg", "6x2.25ltr",
// "5x25x40gm", "60x6pcs". Lengths ("6x45cm") are no amount of anything.
const sizeRe = /(\d+(?:\.\d+)?(?:\s*[x×*]\s*\d+(?:\.\d+)?)*)\s*(kg|gms|gm|g|ltr|litre|liter|l|ml|pcs|pieces|pc|cm|mm|m)\b/g;
const lengths = new Set(["cm", "mm", "m"]);
const pieceWords = new Set(["pcs", "pieces", "pc"]);

export type Size = {
  /** In grams, millilitres or pieces. */
  amount: number;
  unit: Unit;
  /** How many items, when the size says so: 5x25x40gm is 125 of 40 g. */
  count?: number;
  text: string;
};

/**
 * The pack size a product's name gives, if it gives one. Of several, the
 * one with the most factors: "SpringRoll 25g 4x1.25kg" is 5 kg, and
 * "48x385ml (410gms)" is 18,480 ml.
 */
export function guessPack(name: string): Size | null {
  const s = name.toLowerCase();
  let best: (Size & { factors: number }) | null = null;
  for (const m of s.matchAll(sizeRe)) {
    const word = m[2]!;
    if (lengths.has(word)) continue;
    const factors = m[1]!.split(/[x×*]/).map((f) => Number(f.trim()));
    const all = factors.reduce((x, f) => x * f, 1);
    let size: Size;
    if (pieceWords.has(word)) {
      size = { amount: round(all), unit: "piece", text: m[0] };
    } else {
      const u = scale[word]!;
      const count = factors.length > 1 ? factors.slice(0, -1).reduce((x, f) => x * f, 1) : undefined;
      size = { amount: round(all * u.by), unit: u.unit, count, text: m[0] };
    }
    if (!best || factors.length > best.factors) best = { ...size, factors: factors.length };
  }
  if (best) {
    const { factors: _, ...size } = best;
    return size;
  }
  // "Paper cups, 8 oz x 1000": a count alone, unless it is a length.
  const m = /[x×]\s*(\d+)\b(?!\s*(?:cm|mm|m)\b)/.exec(s);
  return m ? { amount: Number(m[1]), unit: "piece", text: m[0] } : null;
}

/**
 * How much one holds, in an ingredient's unit, as the name gives it: for
 * pieces, the count ("Samosa 5x25x40gm" holds 125).
 */
export function packFor(name: string, unit: Unit | undefined): number | null {
  const size = guessPack(name);
  if (!size || !unit) return null;
  if (size.unit === unit) return size.amount;
  if (unit === "piece" && size.count) return size.count;
  return null;
}

const round = (n: number) => Math.round(n * 1000) / 1000;

/** The ingredient a product's name starts with or names, the longest that fits. */
export function guessIngredient(name: string, ingredients: Ingredient[]): Ingredient | undefined {
  const s = ` ${name.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, " ")} `;
  let best: Ingredient | undefined;
  for (const i of ingredients) {
    const n = i.name.toLowerCase().replace(/[^\p{L}\p{N}]+/gu, " ").trim();
    if (n && s.includes(` ${n} `) && (!best || n.length > best.name.length)) best = i;
  }
  return best;
}

/** A name for a new ingredient: the product's name without its size. */
export function guessIngredientName(name: string): string {
  const size = guessPack(name);
  let s = name;
  if (size) s = s.replace(new RegExp(size.text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "i"), "");
  return s.split(",")[0]!.replace(/\s+/g, " ").replace(/[\s,x×-]+$/i, "").trim();
}
