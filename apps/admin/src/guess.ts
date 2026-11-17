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
  /** One piece's weight or volume, when the name gives it apart: "25g 4x1.25kg" is 25 g. */
  each?: number;
  text: string;
};

/** Units an invoice counts whole cartons or cases in: the name's size is all of one. */
const wholeUnits = new Set(["CAR", "CTN", "CRT", "CS", "CASE", "BOX", "BX", "OUT", "OUTER"]);
/** Units that are an amount themselves: one KG is 1,000 g, whatever the name says. */
const measureUnits: Record<string, { unit: Unit; amount: number }> = {
  KG: { unit: "g", amount: 1000 },
  G: { unit: "g", amount: 1 },
  GM: { unit: "g", amount: 1 },
  GMS: { unit: "g", amount: 1 },
  L: { unit: "ml", amount: 1000 },
  LTR: { unit: "ml", amount: 1000 },
  ML: { unit: "ml", amount: 1 },
};

/**
 * The pack size a product's name gives, if it gives one, for one of an
 * invoice's unit. Of several sizes, the one with the most factors:
 * "SpringRoll 25g 4x1.25kg" is 5 kg, and "48x385ml (410gms)" is 18,480 ml.
 * A carton (CAR, CTN, BOX) or no unit is the whole size; another unit, as
 * EA or PAC, is one of what the carton holds: "Mozzarella 6x2kg" by the EA
 * is 2 kg, "Steel wool 60x6pcs" by the PAC is 6 pieces.
 */
export function guessPack(name: string, uom = ""): Size | null {
  const u = uom.trim().toUpperCase();
  const measure = measureUnits[u];
  if (measure) return { amount: measure.amount, unit: measure.unit, text: "" };
  const s = name.toLowerCase();
  type Found = { factors: number[]; word: string; text: string };
  const found: Found[] = [];
  for (const m of s.matchAll(sizeRe)) {
    const word = m[2]!;
    if (lengths.has(word)) continue;
    found.push({ factors: m[1]!.split(/[x×*]/).map((f) => Number(f.trim())), word, text: m[0] });
  }
  if (found.length) {
    const best = found.reduce((a, b) => (b.factors.length > a.factors.length ? b : a));
    let factors = best.factors;
    // One of what a carton holds: without its outer count.
    const split = u !== "" && !wholeUnits.has(u) && factors.length > 1;
    if (split) factors = factors.slice(1);
    const all = factors.reduce((x, f) => x * f, 1);
    if (pieceWords.has(best.word)) return { amount: round(all), unit: "piece", text: best.text };
    const sc = scale[best.word]!;
    const amount = round(all * sc.by);
    // The weight of one piece, given apart: "25g 4x1.25kg" is 200 of 25 g.
    const piece = found.find((f) => f !== best && f.factors.length === 1 && !pieceWords.has(f.word) &&
      scale[f.word]?.unit === sc.unit);
    const one = piece ? piece.factors[0]! * scale[piece.word]!.by : 0;
    let count: number | undefined;
    let each: number | undefined;
    if (one > 0 && one < amount) {
      count = Math.round(amount / one);
      each = one;
    }
    else if (factors.length > 1) count = factors.slice(0, -1).reduce((x, f) => x * f, 1);
    else if (split) count = 1;
    return { amount, unit: sc.unit, count, each, text: best.text };
  }
  // "Paper cups, 8 oz x 1000": a count alone, unless it is a length.
  const m = /[x×]\s*(\d+)\b(?!\s*(?:cm|mm|m)\b)/.exec(s);
  return m ? { amount: Number(m[1]), unit: "piece", text: m[0] } : null;
}

/**
 * How much one of an invoice's unit holds, in an ingredient's unit, as the
 * name gives it: for pieces, the count ("Samosa 5x25x40gm" holds 125).
 */
export function packFor(name: string, unit: Unit | undefined, uom = ""): number | null {
  const size = guessPack(name, uom);
  if (!size || !unit) return null;
  if (size.unit === unit) return size.amount;
  if (unit === "piece" && size.count) return size.count;
  return null;
}

/**
 * What a new ingredient is likely measured in: pieces for what is sold or
 * used whole (cans, bottles, "5x25x40gm" samosas, "25g 4x1.25kg" rolls),
 * else the unit of the name's size.
 */
export function suggestUnit(name: string): Unit {
  if (/\b(cans?|bottles?|btls?|tins?|cups?|pcs|pieces?)\b/i.test(name)) return "piece";
  const size = guessPack(name);
  if (!size) return "g";
  if (size.each || size.text.split(/[x×*]/).length >= 3) return "piece";
  return size.unit;
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
