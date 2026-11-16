import { A, Navigate, Route, Router, useNavigate, type RouteSectionProps } from "@solidjs/router";
import { For, Match, onMount, Switch } from "solid-js";
import { api, call, whenSignedOut } from "./api/client";
import { IngredientsPage } from "./pages/IngredientsPage";
import { MenuPage } from "./pages/MenuPage";
import { OptionsPage } from "./pages/OptionsPage";
import { PurchasesPage } from "./pages/PurchasesPage";
import { RecipesPage } from "./pages/RecipesPage";
import { SalesPage } from "./pages/SalesPage";
import { SignInPage } from "./pages/SignInPage";
import { StaffPage } from "./pages/StaffPage";
import { TillsPage } from "./pages/TillsPage";
import { me, setMe, loadMe } from "./state";

export function App() {
  return (
    <Router root={Root}>
      <Route path="/sign-in" component={SignInPage} />
      <Route path="/" component={SignedIn}>
        <Route path="/" component={() => <Navigate href="/sales" />} />
        <Route path="/sales" component={SalesPage} />
        <Route path="/purchases" component={PurchasesPage} />
        <Route path="/ingredients" component={IngredientsPage} />
        <Route path="/recipes" component={RecipesPage} />
        <Route path="/menu" component={MenuPage} />
        <Route path="/options" component={OptionsPage} />
        <Route path="/staff" component={StaffPage} />
        <Route path="/tills" component={TillsPage} />
      </Route>
      <Route path="*" component={() => <Navigate href="/sales" />} />
    </Router>
  );
}

function Root(props: RouteSectionProps) {
  const navigate = useNavigate();
  whenSignedOut(() => {
    setMe(null);
    navigate("/sign-in", { replace: true });
  });
  return <>{props.children}</>;
}

/** The pages that need someone signed in, under the top bar. */
function SignedIn(props: RouteSectionProps) {
  onMount(() => {
    if (me() === undefined) void loadMe();
  });
  return (
    <Switch>
      <Match when={me() === undefined}>
        <p class="loading">Opening the admin.</p>
      </Match>
      <Match when={me() === null}>
        <Navigate href="/sign-in" />
      </Match>
      <Match when={me()}>
        <TopBar />
        {props.children}
      </Match>
    </Switch>
  );
}

function TopBar() {
  const navigate = useNavigate();
  const signOut = async () => {
    try {
      await call(api.DELETE("/v1/admin/session"));
    } finally {
      setMe(null);
      navigate("/sign-in", { replace: true });
    }
  };
  return (
    <header class="topbar">
      <div class="brand">
        <strong>{me()?.organization.name}</strong>
        <For each={me()?.shops}>{(shop) => <span class="muted">{shop.name}</span>}</For>
      </div>
      <nav class="nav" aria-label="Sections">
        <A href="/sales">Sales</A>
        <A href="/purchases">Purchases</A>
        <A href="/ingredients">Ingredients</A>
        <A href="/recipes">Recipes</A>
        <A href="/menu">Menu</A>
        <A href="/options">Options</A>
        <A href="/staff">Staff</A>
        <A href="/tills">Tills</A>
      </nav>
      <span class="spacer" />
      <span class="muted who">{me()?.user.name}</span>
      <button type="button" class="btn btn-plain" onClick={signOut}>
        Sign out
      </button>
    </header>
  );
}
