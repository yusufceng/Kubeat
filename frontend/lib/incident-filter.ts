/*
 * The incident list's filter as it lives in the address: which incidents,
 * in which order, which page. The page, its links and the CSV export read
 * it the same way. No imports but types, so that the tests run it as it is.
 */
import type { IncidentQuery } from "@/lib/api";

export type IncidentSort = "started_at" | "duration" | "monitor";

export type IncidentFilter = {
  /** Only these namespaces; "" stands for the monitors without one. Empty for all. */
  namespaces: string[];
  kind: "down" | "cert" | null;
  state: "open" | "resolved" | null;
  monitorId: number | null;
  /** Text the monitor's name contains; "" for any. */
  q: string;
  /** One of the offered periods up to now, in seconds; null for none. */
  range: number | null;
  /** A period of its own (RFC 3339), used when range is null; either end may be open. */
  from: string | null;
  to: string | null;
  sort: IncidentSort;
  /** The direction in effect, whether the address names it or the sort's own is meant. */
  order: "asc" | "desc";
  /** 1 for the first. */
  page: number;
};

/** The longest text the backend matches names against. */
export const QUERY_MAX = 100;

/** Each sort's own direction: the newest, the longest, A to Z. */
export const defaultOrder: Record<IncidentSort, "asc" | "desc"> = {
  started_at: "desc",
  duration: "desc",
  monitor: "asc",
};

export const emptyFilter: IncidentFilter = {
  namespaces: [],
  kind: null,
  state: null,
  monitorId: null,
  q: "",
  range: null,
  from: null,
  to: null,
  sort: "started_at",
  order: "desc",
  page: 1,
};

type Params = URLSearchParams | Record<string, string | string[] | undefined>;

function all(params: Params, name: string): string[] {
  if (params instanceof URLSearchParams) return params.getAll(name);
  const value = params[name];
  return value === undefined ? [] : Array.isArray(value) ? value : [value];
}

const first = (params: Params, name: string): string | undefined => all(params, name)[0];

/** "24h", "7d", "90m": a period as the address carries it, in hours up to two days. */
export function rangeKey(seconds: number): string {
  if (seconds % 86400 === 0 && seconds >= 2 * 86400) return `${seconds / 86400}d`;
  if (seconds % 3600 === 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}

/** A moment the backend can read (RFC 3339), or null. */
function moment(value: string | undefined): string | null {
  if (!value) return null;
  const ms = Date.parse(value);
  // Date.parse also reads loose forms ("2026"); only a full date with a time goes on.
  return Number.isFinite(ms) && /^\d{4}-\d{2}-\d{2}T/.test(value) ? new Date(ms).toISOString() : null;
}

/**
 * The filter in an address's query. What it cannot use is left out rather
 * than refused: an unknown kind, a period that is not offered, a page
 * below 1. ranges are the periods offered, in seconds.
 */
export function parseIncidentFilter(params: Params, ranges: number[]): IncidentFilter {
  const kind = first(params, "kind");
  const state = first(params, "state");
  const monitor = Number(first(params, "monitor"));
  const sort = first(params, "sort");
  const order = first(params, "order");
  const page = Number(first(params, "page"));
  const range = ranges.find((r) => rangeKey(r) === first(params, "range")) ?? null;
  let from = range === null ? moment(first(params, "from")) : null;
  let to = range === null ? moment(first(params, "to")) : null;
  // The backend refuses a period that ends before it starts.
  if (from && to && Date.parse(to) <= Date.parse(from)) [from, to] = [null, null];
  const sortKey: IncidentSort = sort === "duration" || sort === "monitor" ? sort : "started_at";
  return {
    namespaces: [...new Set(all(params, "namespace").map((ns) => ns.trim()))],
    kind: kind === "down" || kind === "cert" ? kind : null,
    state: state === "open" || state === "resolved" ? state : null,
    monitorId: Number.isInteger(monitor) && monitor > 0 ? monitor : null,
    q: (first(params, "q") ?? "").trim().slice(0, QUERY_MAX),
    range,
    from,
    to,
    sort: sortKey,
    order: order === "asc" || order === "desc" ? order : defaultOrder[sortKey],
    page: Number.isInteger(page) && page > 1 ? page : 1,
  };
}

/** Whether anything narrows the list (the order and the page do not). */
export function isFiltered(f: IncidentFilter): boolean {
  return (
    f.namespaces.length > 0 ||
    f.kind !== null ||
    f.state !== null ||
    f.monitorId !== null ||
    f.q !== "" ||
    f.range !== null ||
    f.from !== null ||
    f.to !== null
  );
}

/**
 * What to ask the backend for. now ends an offered period; pageSize, when
 * given, picks the page (the export asks for every incident).
 */
export function filterToQuery(f: IncidentFilter, now: number, pageSize?: number): IncidentQuery {
  return {
    namespaces: f.namespaces,
    kind: f.kind ?? undefined,
    state: f.state ?? undefined,
    monitorId: f.monitorId ?? undefined,
    q: f.q || undefined,
    from: f.range !== null ? new Date(now - f.range * 1000).toISOString() : (f.from ?? undefined),
    to: f.range !== null ? undefined : (f.to ?? undefined),
    sort: f.sort === "started_at" ? undefined : f.sort,
    order: f.order === defaultOrder[f.sort] ? undefined : f.order,
    ...(pageSize !== undefined && { limit: pageSize, offset: (f.page - 1) * pageSize }),
  };
}

/**
 * The address's query for a filter, with changes applied; what is as it
 * would be anyway is left out, so that addresses stay short. A change to
 * anything but the page starts again at the first page; a new sort comes
 * in its own direction unless one is given.
 */
export function filterParams(f: IncidentFilter, changes: Partial<IncidentFilter> = {}): URLSearchParams {
  const restart = Object.keys(changes).some((k) => k !== "page");
  const next: IncidentFilter = { ...f, ...(restart && { page: 1 }), ...changes };
  if (changes.sort && !changes.order) next.order = defaultOrder[changes.sort];
  const params = new URLSearchParams();
  if (next.q) params.set("q", next.q);
  if (next.state) params.set("state", next.state);
  if (next.kind) params.set("kind", next.kind);
  for (const ns of next.namespaces) params.append("namespace", ns);
  if (next.monitorId !== null) params.set("monitor", String(next.monitorId));
  if (next.range !== null) params.set("range", rangeKey(next.range));
  else {
    if (next.from) params.set("from", next.from);
    if (next.to) params.set("to", next.to);
  }
  if (next.sort !== "started_at") params.set("sort", next.sort);
  if (next.order !== defaultOrder[next.sort]) params.set("order", next.order);
  if (next.page > 1) params.set("page", String(next.page));
  return params;
}

/** The list's address for a filter, with changes applied. */
export function filterHref(f: IncidentFilter, changes: Partial<IncidentFilter> = {}): string {
  const query = filterParams(f, changes).toString();
  return query ? `/incidents?${query}` : "/incidents";
}

/** The export's address: the same incidents, all of them, in the same order. */
export function csvHref(f: IncidentFilter): string {
  const query = filterParams(f, { page: 1 }).toString();
  return query ? `/incidents/csv?${query}` : "/incidents/csv";
}
