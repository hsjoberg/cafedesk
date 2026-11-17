import { useNavigate } from "@solidjs/router";
import { createSignal, Show } from "solid-js";
import { api, call } from "../api/client";
import { setMe } from "../state";

export function SignInPage() {
  const navigate = useNavigate();
  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [busy, setBusy] = createSignal(false);

  const submit = async (e: SubmitEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      setMe(await call(api.POST("/v1/admin/session", { body: { username: username(), password: password() } })));
      navigate("/sales", { replace: true });
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <main class="signin">
      <form onSubmit={submit} novalidate>
        <div>
          <h1>Cafedesk</h1>
          <p class="muted">Sign in to manage your menu.</p>
        </div>
        <label class="field">
          <span>Username</span>
          <input class="input" type="text" autocomplete="username" autocapitalize="none" spellcheck={false} required
            value={username()} onInput={(e) => setUsername(e.currentTarget.value)} />
        </label>
        <label class="field">
          <span>Password</span>
          <input class="input" type="password" autocomplete="current-password" required value={password()}
            onInput={(e) => setPassword(e.currentTarget.value)} />
        </label>
        <Show when={error()}>
          <p class="error" role="alert">{error()}</p>
        </Show>
        <button type="submit" class="btn btn-ink" disabled={busy() || !username() || !password()}>
          Sign in
        </button>
      </form>
    </main>
  );
}
