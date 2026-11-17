import { Show } from "solid-js";
import { formatFils } from "../money";

/** An item as the till draws it: signage band, names, price. */
export function Tile(props: {
  name: string;
  nameAr: string;
  priceFils: number | null;
  color: string;
  groups?: string;
  selected?: boolean;
  draft?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      class="tile"
      classList={{ draft: props.draft }}
      aria-pressed={props.selected ? "true" : "false"}
      style={{ "--band": props.color }}
      onClick={() => props.onClick?.()}
    >
      <span class="band" />
      <span class="body">
        <span class="name">{props.name || "New item"}</span>
        <Show when={props.nameAr}>
          <span class="name-ar" lang="ar" dir="rtl">
            {props.nameAr}
          </span>
        </Show>
        <Show when={props.groups}>
          <span class="groups">{props.groups}</span>
        </Show>
        <span class="price">{props.priceFils === null ? "—" : formatFils(props.priceFils)}</span>
      </span>
    </button>
  );
}
