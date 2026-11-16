import { createSignal } from "solid-js";
import { api, call, type Ingredient, type RecipeLine, type Recipes } from "./api/client";
import { unitOf } from "./areas";

// What a recipe is made of, in words, for the pages that show a cost from
// it without editing it: Menu and Options.

const grouped = new Intl.NumberFormat("en", { maximumFractionDigits: 3 });

const [recipes, setRecipes] = createSignal<Recipes>();
const [ingredients, setIngredients] = createSignal<Ingredient[]>([]);

/** Loads the recipes and ingredients, for recipeOf and recipeText. */
export async function loadRecipes() {
  try {
    const [r, i] = await Promise.all([call(api.GET("/v1/admin/recipes")), call(api.GET("/v1/admin/ingredients"))]);
    setRecipes(r);
    setIngredients(i);
  } catch {
    // Only words beside a cost; the cost shows without them.
  }
}

export const itemRecipeLines = (id: string) => recipes()?.items.find((r) => r.itemId === id)?.lines;
export const optionRecipeLines = (id: string) => recipes()?.modifiers.find((r) => r.modifierId === id)?.lines;

/** A recipe's lines in words: "Chicken curry base, 1 portion", "Sugar, -10 g". */
export function recipeText(lines: RecipeLine[] | undefined): string[] {
  return (lines ?? []).map((l) => {
    const n = grouped.format(l.amount);
    const one = Math.abs(l.amount) === 1;
    if (l.batchId) {
      const b = recipes()?.batches.find((x) => x.id === l.batchId);
      return `${b?.name ?? "A batch"}, ${n} ${one ? "portion" : "portions"}`;
    }
    const i = ingredients().find((x) => x.id === l.ingredientId);
    if (!i) return `${n}`;
    const unit = i.unit === "piece" && one ? "piece" : unitOf(i.unit).short;
    return `${i.name}, ${n} ${unit}`;
  });
}
