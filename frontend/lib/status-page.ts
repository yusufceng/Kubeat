/*
 * What the status pages (the editor and the public page) work out on their
 * own: the kinds an update can be and how each is drawn, the page's states
 * from calm to grave, which monitors just went down, and the days a list
 * of updates falls on. No imports but types, so that the tests run it as
 * it is.
 */
import type { StatusPageUpdateKind, StatusPageView } from "@/lib/api";

/** An update's kind as visitors see it: the six an editor writes, and an incident note shown on the page. */
export type UpdateKind = StatusPageUpdateKind | "note";

/**
 * How a kind is drawn: a state glyph (app/ui/status-glyph.tsx) where the
 * kind is a stage of an outage, an icon where it is a plain word.
 */
export type UpdateMark = "up" | "down" | "warn" | "maint" | "pending" | "notice" | "note";

/** The kinds an editor picks from, in the order of an outage's life, then the two that stand alone. */
export const updateKinds: StatusPageUpdateKind[] = ["investigating", "identified", "monitoring", "resolved", "notice", "maintenance"];

export const updateMarks: Record<UpdateKind, UpdateMark> = {
  investigating: "down",
  identified: "warn",
  monitoring: "pending",
  resolved: "up",
  notice: "notice",
  maintenance: "maint",
  note: "note",
};

/** The longest title (in bytes) and body (in characters) the backend keeps (API revision 27). */
export const UPDATE_TITLE_MAX_BYTES = 100;
export const UPDATE_BODY_MAX = 4000;

/** How many bytes a string takes in UTF-8, which is how the backend measures a title. */
export function utf8Length(text: string): number {
  let bytes = 0;
  for (const ch of text) {
    const code = ch.codePointAt(0)!;
    bytes += code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
  }
  return bytes;
}

export type PageState = StatusPageView["state"];

/** The page's states from calm to grave; a wall screen sounds when the page climbs it. */
const stateRank: Record<PageState, number> = { up: 0, maintenance: 1, degraded: 2, down: 3 };

export function worsened(before: PageState, after: PageState): boolean {
  return stateRank[after] > stateRank[before];
}

/** The monitors down now that were not down before, by id. */
export function newlyDown(before: Iterable<number>, now: Iterable<number>): number[] {
  const known = new Set(before);
  return [...now].filter((id) => !known.has(id));
}

/** The calendar day a moment falls on, as a key: in the viewer's zone, or in UTC. */
export function dayKey(iso: string, local: boolean): string {
  if (!local) return iso.slice(0, 10);
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/**
 * Updates grouped by the day they were written, newest day first and
 * within a day newest first, whatever order they came in. `local` groups
 * by the viewer's day (once the browser has taken over); until then by
 * UTC, which the backend's own grouping also uses.
 */
export function byDay<U extends { created_at: string }>(updates: U[], local: boolean): { day: string; updates: U[] }[] {
  const sorted = [...updates].sort((a, b) => (a.created_at < b.created_at ? 1 : a.created_at > b.created_at ? -1 : 0));
  const days: { day: string; updates: U[] }[] = [];
  for (const u of sorted) {
    const day = dayKey(u.created_at, local);
    const last = days[days.length - 1];
    if (last && last.day === day) last.updates.push(u);
    else days.push({ day, updates: [u] });
  }
  return days;
}

/** localStorage key of the wall screen choice on a public page; "on" when chosen. */
export const WALL_KEY = "kubeat-wall";

/**
 * Runs before the first paint of a public page: marks the document for
 * the wall screen when the visitor chose it, so that the page does not
 * open small and then grow (app/styles/statuspage.css reads the mark).
 */
export const wallScript = `(function(){try{if(localStorage.getItem("${WALL_KEY}")==="on")document.documentElement.dataset.wall="on"}catch(e){}})()`;

/** Whether a maintenance window is going on at `now` (milliseconds), rather than still ahead. */
export function inProgress(window: { starts_at: string; ends_at: string }, now: number): boolean {
  return Date.parse(window.starts_at) <= now && now < Date.parse(window.ends_at);
}
