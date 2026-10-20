// What the overview, the monitor list and a namespace's page make of the
// monitors and incidents they read: rows, outages, the strip's inputs, the
// activity feed. Everything here is plain data, handed on to the browser.
import "server-only";

import type { Incident, Maintenance, Monitor } from "@/lib/api";
import type { StripOutage, StripWindow } from "@/lib/fleet-strip";
import { kindShort, objectRef } from "@/lib/kinds";
import { type ListItem, toListItem } from "@/lib/workspace";
import { monitorLabel } from "@/app/ui/monitor-name";
import { displayState } from "@/app/ui/status";

/** Longer errors are cut here; the monitor's page has them whole. */
const ERROR_CHARS = 240;

/**
 * The moment a page reads its data. The fleet strip and the feed's "today"
 * are measured from it, and it goes to the browser with the data, so that
 * the server and the browser draw the same thing. A server component
 * renders once per request, so this cannot drift between renders.
 */
export function readTime(): number {
  return Date.now();
}

/** A monitor as the list and the cluster map show it. */
export type FleetItem = ListItem & {
  /** Found in the cluster (it has a source), rather than entered by hand. */
  discovered: boolean;
  /** The kind as a short tag: svc, route, ing, deploy, sts, ds; or the check: http, tcp, push, ... */
  kindShort: string;
  /** The object it follows, as kubectl writes it: "svc/pricing:8080"; null when entered by hand. */
  object: string | null;
  /** Seconds between checks. */
  interval: number;
  /** What the latest check failed with, when it failed. */
  error: string | null;
  /** Whole days left on the certificate while the backend warns about it (or once it expired); else null. */
  certDays: number | null;
  /** When the ongoing outage began. */
  downSince: string | null;
};

export function toFleetItem(m: Monitor): FleetItem {
  const last = m.last_result;
  const error = last && !last.up ? (last.error ?? "").slice(0, ERROR_CHARS) || null : null;
  let certDays: number | null = null;
  if (m.enabled && last?.cert_expires_at) {
    // As of the latest check, so that the figure does not depend on the clock of whoever renders it.
    const days = Math.floor((Date.parse(last.cert_expires_at) - Date.parse(last.checked_at)) / 86_400_000);
    if (m.cert_warning || days < 0) certDays = days;
  }
  return {
    ...toListItem(m),
    discovered: m.source !== "",
    kindShort: kindShort(m.source, m.type, m.target),
    object: objectRef(m.source),
    interval: m.interval_seconds,
    error,
    certDays,
    downSince: m.open_incident?.started_at ?? null,
  };
}

/** Uptime over every monitor, weighted by their checks, and the period it covers. */
export function fleetUptime(monitors: Monitor[]): { uptime: number | null; window: number } {
  let checks = 0;
  let up = 0;
  for (const m of monitors) {
    if (m.uptime_percent === null) continue;
    checks += m.checks;
    up += (m.checks * m.uptime_percent) / 100;
  }
  return { uptime: checks > 0 ? (100 * up) / checks : null, window: monitors[0]?.window_seconds ?? 0 };
}

/** When the most recent check of any of the monitors ran. */
export function lastChecked(monitors: Monitor[]): string | null {
  let latest: string | null = null;
  for (const m of monitors) {
    const at = m.last_result?.checked_at;
    if (at && (latest === null || at > latest)) latest = at;
  }
  return latest;
}

/** An outage that is going on, as the summary lists it. */
export type Outage = { id: number; name: string; namespace: string; cause: string; since: string };

/**
 * The ongoing outages, the newest first: monitors that are down, as the
 * summary's sentence counts them. One that passes its checks again while
 * its outage is still open is recovering, and counted as degraded.
 */
export function ongoingOutages(monitors: Monitor[]): Outage[] {
  return monitors
    .filter((m) => m.open_incident !== null && displayState(m) === "down")
    .map((m) => ({
      id: m.id,
      name: monitorLabel(m).name,
      namespace: m.namespace,
      cause: m.open_incident!.cause,
      since: m.open_incident!.started_at,
    }))
    .sort((a, b) => b.since.localeCompare(a.since));
}

/** The certificates the monitors watch: how many, how many the backend warns about, and the soonest to expire. */
export function certificateSummary(monitors: Monitor[]): { watched: number; warned: number; soonest: number | null } {
  let watched = 0;
  let warned = 0;
  let soonest: number | null = null;
  for (const m of monitors) {
    const last = m.last_result;
    if (!m.enabled || !last?.cert_expires_at) continue;
    watched++;
    const days = Math.floor((Date.parse(last.cert_expires_at) - Date.parse(last.checked_at)) / 86_400_000);
    if (m.cert_warning || days < 0) warned++;
    if (soonest === null || days < soonest) soonest = days;
  }
  return { watched, warned, soonest };
}

/**
 * What the fleet strip is drawn from: the outages among the incidents read
 * (and those the monitors say are open, in case the list was cut before
 * them), the maintenance windows, and from when on the data is complete.
 */
export function stripInputs(
  monitors: Monitor[],
  incidents: Incident[],
  windows: Maintenance[],
  incidentsLimit: number,
): { outages: StripOutage[]; windows: StripWindow[]; knownFrom: number } {
  const names = new Map(monitors.map((m) => [m.id, monitorLabel(m).name]));
  const seen = new Set<number>();
  const outages: StripOutage[] = [];
  const add = (i: Incident) => {
    if (i.kind !== "down" || seen.has(i.id)) return;
    seen.add(i.id);
    outages.push({
      name: names.get(i.monitor_id) ?? i.monitor_name,
      start: Date.parse(i.started_at),
      end: i.resolved_at ? Date.parse(i.resolved_at) : null,
    });
  };
  incidents.forEach(add);
  monitors.forEach((m) => m.open_incident && add(m.open_incident));

  // Nothing is known before the oldest monitor was created, nor before the
  // oldest incident read when the list was cut at its limit.
  let knownFrom = Math.min(...monitors.map((m) => Date.parse(m.created_at)));
  if (incidents.length >= incidentsLimit) {
    knownFrom = Math.max(knownFrom, Math.min(...incidents.map((i) => Date.parse(i.started_at))));
  }
  return {
    outages,
    windows: windows.map((w) => ({
      name: w.name,
      start: Date.parse(w.starts_at),
      end: Date.parse(w.ends_at),
      repeat: w.repeat,
    })),
    knownFrom: Number.isFinite(knownFrom) ? knownFrom : 0,
  };
}

/** The maintenance windows that cover any of these monitors: theirs, their namespace's, or everyone's. */
export function windowsFor(windows: Maintenance[], monitors: Monitor[]): Maintenance[] {
  const ids = new Set(monitors.map((m) => m.id));
  const namespaces = new Set(monitors.map((m) => m.namespace));
  return windows.filter((w) =>
    w.monitor_id !== null ? ids.has(w.monitor_id) : w.namespace === "" || namespaces.has(w.namespace),
  );
}

/** One line of the activity feed: an outage or a certificate warning that began or ended. */
export type ActivityEvent = {
  key: string;
  at: string;
  monitorId: number;
  name: string;
  kind: "down" | "recovered" | "cert" | "renewed";
  /** The cause, for a beginning. */
  cause: string;
  /** How long it lasted, in milliseconds, for an ending. */
  lasted: number | null;
};

/** The beginnings and endings of the incidents, newest first. */
export function activity(incidents: Incident[], monitors: Monitor[], limit: number): ActivityEvent[] {
  const names = new Map(monitors.map((m) => [m.id, monitorLabel(m).name]));
  const out: ActivityEvent[] = [];
  for (const i of incidents) {
    const cert = i.kind === "cert";
    const name = names.get(i.monitor_id) ?? i.monitor_name;
    out.push({
      key: `${i.id}-start`,
      at: i.started_at,
      monitorId: i.monitor_id,
      name,
      kind: cert ? "cert" : "down",
      cause: i.cause,
      lasted: null,
    });
    if (i.resolved_at) {
      out.push({
        key: `${i.id}-end`,
        at: i.resolved_at,
        monitorId: i.monitor_id,
        name,
        kind: cert ? "renewed" : "recovered",
        cause: "",
        lasted: Date.parse(i.resolved_at) - Date.parse(i.started_at),
      });
    }
  }
  return out.sort((a, b) => b.at.localeCompare(a.at)).slice(0, limit);
}
