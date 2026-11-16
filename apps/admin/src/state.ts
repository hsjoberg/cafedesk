import { createSignal } from "solid-js";
import { api, call, type Catalog, type Me } from "./api/client";

// What the admin knows: who is signed in, and the menu.

/** Undefined while loading, null when no one is signed in. */
export const [me, setMe] = createSignal<Me | null | undefined>(undefined);

export async function loadMe() {
  try {
    setMe(await call(api.GET("/v1/admin/me")));
  } catch {
    setMe(null);
  }
}

export const [catalog, setCatalog] = createSignal<Catalog>();
export const [catalogError, setCatalogError] = createSignal("");

export async function reloadCatalog() {
  try {
    setCatalog(await call(api.GET("/v1/admin/catalog")));
    setCatalogError("");
  } catch (e) {
    setCatalogError((e as Error).message);
  }
}
