// Business dates, as YYYY-MM-DD strings in a shop's time zone. Arithmetic
// is done at midnight UTC, where no day is 23 or 25 hours long.

const ymd = (d: Date) => d.toISOString().slice(0, 10);
const utc = (date: string) => new Date(date + "T00:00:00Z");

/** Today's date in a time zone. */
export function todayIn(timeZone: string): string {
  try {
    return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  } catch {
    return ymd(new Date());
  }
}

export function addDays(date: string, n: number): string {
  const d = utc(date);
  d.setUTCDate(d.getUTCDate() + n);
  return ymd(d);
}

export function monthStart(date: string): string {
  return date.slice(0, 8) + "01";
}

/** The number of dates from one to another, both counted. */
export function daysBetween(from: string, to: string): number {
  return Math.round((utc(to).getTime() - utc(from).getTime()) / 86_400_000) + 1;
}

export function isDate(s: string | undefined): s is string {
  return !!s && /^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(utc(s).getTime());
}

/** "Saturday 14 November", or with the year when it isn't this one's. */
export function longDate(date: string, thisYear?: string): string {
  return utc(date).toLocaleDateString(undefined, {
    timeZone: "UTC",
    weekday: "long",
    day: "numeric",
    month: "long",
    year: thisYear && date.slice(0, 4) === thisYear.slice(0, 4) ? undefined : "numeric",
  });
}

/** "14 Nov", or "Sat 14 Nov" with the weekday. */
export function shortDate(date: string, weekday = false): string {
  return utc(date).toLocaleDateString(undefined, {
    timeZone: "UTC",
    weekday: weekday ? "short" : undefined,
    day: "numeric",
    month: "short",
  });
}

/** A range in words: one date, or the first and last. */
export function rangeLabel(from: string, to: string, today: string): string {
  if (from === to) return longDate(from, today);
  const sameYear = from.slice(0, 4) === to.slice(0, 4) && from.slice(0, 4) === today.slice(0, 4);
  const opts: Intl.DateTimeFormatOptions = { timeZone: "UTC", day: "numeric", month: "short", year: sameYear ? undefined : "numeric" };
  return `${utc(from).toLocaleDateString(undefined, opts)} to ${utc(to).toLocaleDateString(undefined, opts)}`;
}

export type Preset = { id: string; label: string; range: (today: string) => [string, string] };

export const presets: Preset[] = [
  { id: "today", label: "Today", range: (t) => [t, t] },
  { id: "yesterday", label: "Yesterday", range: (t) => [addDays(t, -1), addDays(t, -1)] },
  { id: "week", label: "Last 7 days", range: (t) => [addDays(t, -6), t] },
  { id: "month", label: "This month", range: (t) => [monthStart(t), t] },
  {
    id: "last-month",
    label: "Last month",
    range: (t) => {
      const end = addDays(monthStart(t), -1);
      return [monthStart(end), end];
    },
  },
];
