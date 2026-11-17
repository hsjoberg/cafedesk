import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { currencyCode, formatAmount, unit } from "../money";

export type Column = {
  /** For the tooltip and the screen reader: "Saturday 14 November". */
  label: string;
  /** Under the column. Some are left out where the columns are narrow. */
  tick: string;
  /** Fils. */
  value: number;
  /** A second line for the tooltip: "23 orders". */
  detail: string;
};

/** A clean step for an axis reaching max: 1, 2, 2.5 or 5 times a power of ten. */
function niceStep(max: number, steps: number): number {
  const raw = max / steps;
  const pow = 10 ** Math.floor(Math.log10(raw));
  return ([1, 2, 2.5, 5, 10].find((m) => m * pow >= raw) ?? 10) * pow;
}

const whole = new Intl.NumberFormat("en", { maximumFractionDigits: 0 });
const compact = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });

/**
 * Columns of one series of amounts, from one baseline. Each column is its own
 * hit target, with its amount on hover and on keyboard focus; the page gives
 * the same numbers as a table too.
 */
export function ColumnChart(props: { columns: Column[]; label: string }) {
  const [hover, setHover] = createSignal<number | null>(null);
  // Ticks are spaced by the plot's width, so their labels never collide.
  let plot!: HTMLDivElement;
  const [width, setWidth] = createSignal(600);
  onMount(() => {
    const ro = new ResizeObserver(() => setWidth(plot.clientWidth));
    ro.observe(plot);
    onCleanup(() => ro.disconnect());
  });
  const every = () => {
    const longest = Math.max(...props.columns.map((c) => c.tick.length), 1);
    const room = longest * 8 + 14; // px a label needs, at 13 px
    return Math.max(1, Math.ceil(room / (width() / Math.max(n(), 1))));
  };
  const axis = createMemo(() => {
    const max = Math.max(...props.columns.map((c) => c.value), 1) / unit(); // whole units, such as dirhams
    const step = niceStep(max, 4);
    const top = Math.ceil(max / step) * step;
    const ticks: number[] = [];
    for (let v = 0; v <= top + step / 2; v += step) ticks.push(v);
    const fmt = top >= 10_000 ? compact : whole;
    return { top, ticks: ticks.map((v) => ({ pct: (v / top) * 100, text: fmt.format(v) })) };
  });
  const pct = (fils: number) => (fils / unit() / axis().top) * 100;
  const n = () => props.columns.length;
  const tip = () => {
    const i = hover();
    if (i === null || !props.columns[i]) return null;
    const c = props.columns[i];
    const center = ((i + 0.5) / n()) * 100;
    // Near either edge the tooltip leans inwards instead of overflowing.
    const align = center < 15 ? "start" : center > 85 ? "end" : "middle";
    return { c, center, align, bottom: pct(c.value) };
  };

  return (
    <div class="chart" role="group" aria-label={props.label}>
      <div class="chart-y" aria-hidden="true">
        <For each={axis().ticks}>{(t) => <span style={{ bottom: `${t.pct}%` }}>{t.text}</span>}</For>
      </div>
      <div class="chart-plot" ref={plot} onPointerLeave={() => setHover(null)}>
        <For each={axis().ticks}>
          {(t, i) => <div class="chart-grid" classList={{ base: i() === 0 }} style={{ bottom: `${t.pct}%` }} />}
        </For>
        <div class="chart-cols">
          <For each={props.columns}>
            {(c, i) => (
              <div
                class="chart-col"
                tabindex="0"
                aria-label={`${c.label}: ${currencyCode()} ${formatAmount(c.value)}, ${c.detail}`}
                onPointerEnter={() => setHover(i())}
                onFocus={() => setHover(i())}
                onBlur={() => setHover(null)}
                classList={{ on: hover() === i() }}
              >
                <Show when={c.value > 0}>
                  <div class="chart-bar" style={{ height: `${pct(c.value)}%` }} />
                </Show>
              </div>
            )}
          </For>
        </div>
        <Show when={tip()}>
          {(t) => (
            <div
              class={`chart-tip ${t().align}`}
              style={{ left: `${t().center}%`, bottom: `calc(${t().bottom}% + 10px)` }}
              aria-hidden="true"
            >
              <strong>{currencyCode()} {formatAmount(t().c.value)}</strong>
              <span>{t().c.label}</span>
              <span>{t().c.detail}</span>
            </div>
          )}
        </Show>
      </div>
      <div class="chart-x" aria-hidden="true">
        <For each={props.columns}>{(c, i) => <span>{i() % every() === 0 ? c.tick : ""}</span>}</For>
      </div>
    </div>
  );
}
