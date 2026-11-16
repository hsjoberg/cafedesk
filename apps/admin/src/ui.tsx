import { createSignal, onCleanup, Show, type JSX } from "solid-js";
import { ApiError } from "./api/client";

/** A labelled text input, with the error the server gave for it. */
export function Field(props: {
  label: string;
  value: string;
  onInput: (v: string) => void;
  error?: string;
  rtl?: boolean;
  inputMode?: "decimal" | "text";
  placeholder?: string;
  hint?: string;
}) {
  return (
    <label class="field">
      <span>{props.label}</span>
      <input
        class="input"
        value={props.value}
        dir={props.rtl ? "rtl" : undefined}
        lang={props.rtl ? "ar" : undefined}
        inputmode={props.inputMode}
        placeholder={props.placeholder}
        aria-invalid={props.error ? "true" : undefined}
        onInput={(e) => props.onInput(e.currentTarget.value)}
      />
      <Show when={props.hint && !props.error}>
        <span class="meta">{props.hint}</span>
      </Show>
      <Show when={props.error}>
        <span class="error">{props.error}</span>
      </Show>
    </label>
  );
}

/**
 * A delete button that asks for a second tap: the first arms it for four
 * seconds, saying what a second tap will do.
 */
export function DeleteButton(props: { label: string; confirm: string; onDelete: () => void; disabled?: boolean }) {
  const [armed, setArmed] = createSignal(false);
  let timer: number | undefined;
  onCleanup(() => clearTimeout(timer));
  const click = () => {
    if (armed()) {
      setArmed(false);
      props.onDelete();
      return;
    }
    setArmed(true);
    timer = window.setTimeout(() => setArmed(false), 4000);
  };
  return (
    <button type="button" class="btn btn-danger" classList={{ armed: armed() }} disabled={props.disabled} onClick={click}>
      {armed() ? props.confirm : props.label}
    </button>
  );
}

/** The outcome of saving: an error by field, or for the whole form. */
export type SaveError = { field?: string; message: string };

export function toSaveError(e: unknown): SaveError {
  if (e instanceof ApiError) return { field: e.field, message: e.message };
  // A check made before sending: { field, message }.
  const x = e as { field?: unknown; message?: unknown };
  return { field: typeof x.field === "string" ? x.field : undefined, message: String(x.message ?? e) };
}

/** The error for a field, if the save failed on it. */
export function fieldError(err: SaveError | undefined, field: string): string | undefined {
  return err?.field === field ? err.message : undefined;
}

/** The error for the whole form: one no field shows. */
export function FormError(props: { error?: SaveError; fields: string[] }) {
  const shown = () => props.error && !props.error.field?.match(new RegExp(`^(${props.fields.join("|")})$`));
  return (
    <Show when={shown()}>
      <p class="banner" role="alert">
        {props.error!.message}
      </p>
    </Show>
  );
}

export function EditorHead(props: { title: string; onClose: () => void; children?: JSX.Element }) {
  return (
    <div class="editor-head">
      <h2>{props.title}</h2>
      {props.children}
      <button type="button" class="btn btn-option close" onClick={props.onClose}>
        Close
      </button>
    </div>
  );
}
