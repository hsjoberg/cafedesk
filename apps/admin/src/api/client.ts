import createClient from "openapi-fetch";
import type { components, paths } from "./schema";

// The API client, typed from api/openapi.yaml (`bun run gen:api`).

type Schemas = components["schemas"];
export type Me = Schemas["Me"];
export type Catalog = Schemas["Catalog"];
export type Category = Schemas["Category"];
export type CategoryInput = Schemas["CategoryInput"];
export type Item = Schemas["Item"];
export type ItemInput = Schemas["ItemInput"];
export type ModifierGroup = Schemas["ModifierGroup"];
export type Modifier = Schemas["Modifier"];
export type ModifierGroupInput = Schemas["ModifierGroupInput"];
export type ModifierInput = Schemas["ModifierInput"];
export type Staff = Schemas["Staff"];
export type Terminal = Schemas["Terminal"];
export type TerminalPairing = Schemas["TerminalPairing"];
export type SalesReport = Schemas["SalesReport"];
export type DayClose = Schemas["DayClose"];
export type CashMovement = Schemas["CashMovement"];
export type Shop = Schemas["Shop"];
export type VatStatus = Schemas["VatStatus"];
export type Organization = Schemas["Organization"];
export type OrderList = Schemas["OrderList"];
export type OrderRow = Schemas["OrderRow"];
export type Currency = Schemas["Currency"];
export type Area = Schemas["Area"];
export type Product = Schemas["Product"];
export type Supplier = Schemas["Supplier"];
export type Purchase = Schemas["Purchase"];
export type PurchaseInput = Schemas["PurchaseInput"];
export type PurchaseList = Schemas["PurchaseList"];
export type Ingredient = Schemas["Ingredient"];
export type Unit = Schemas["Unit"];
export type Recipes = Schemas["Recipes"];
export type RecipeLine = Schemas["RecipeLine"];
export type Batch = Schemas["Batch"];
export type Usage = Schemas["Usage"];

// Same origin: the server serves the admin, and Vite passes /v1 to it in
// development.
export const api = createClient<paths>({ baseUrl: "" });

/** An error the API answered with, or the network failing. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
    readonly field?: string,
  ) {
    super(message);
  }
}

let onSignedOut = () => {};

/** Sets what happens when the API says the session has ended. */
export function whenSignedOut(fn: () => void) {
  onSignedOut = fn;
}

/** Waits for a call, returning its data or throwing an ApiError. */
export async function call<T>(
  request: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  let result: Awaited<typeof request>;
  try {
    result = await request;
  } catch {
    throw new ApiError("Can't reach the server. Check the connection, then try again.", 0, "network");
  }
  const { data, error, response } = result;
  if (response.ok) return data as T;
  if (response.status === 401) onSignedOut();
  const e = (error as Schemas["Error"] | undefined)?.error;
  throw new ApiError(
    e?.message ?? `The server answered ${response.status}.`,
    response.status,
    e?.code ?? "unknown",
    e?.field,
  );
}
