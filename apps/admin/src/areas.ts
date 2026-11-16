import type { Area, Unit } from "./api/client";

/** Where purchases go, in order, with what their sales are called. */
export const areas: { id: Area; label: string; sales?: string }[] = [
  { id: "counter", label: "Counter", sales: "counter sales" },
  { id: "kitchen", label: "Kitchen", sales: "kitchen sales" },
  { id: "packaging", label: "Packaging" },
  { id: "other", label: "Other" },
];

export const areaLabel = (a: Area) => areas.find((x) => x.id === a)?.label ?? a;

/** Units, as a pack size is written ("8,160 g") and as a price is ("per kg"). */
export const units: { id: Unit; label: string; short: string; per: string; scale: number }[] = [
  { id: "g", label: "Grams", short: "g", per: "per kg", scale: 1000 },
  { id: "ml", label: "Millilitres", short: "ml", per: "per litre", scale: 1000 },
  { id: "piece", label: "Pieces", short: "pieces", per: "each", scale: 1 },
];

export const unitOf = (u: Unit) => units.find((x) => x.id === u)!;
