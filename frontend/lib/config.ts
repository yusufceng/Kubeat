// Server-side settings of the frontend, read from the environment.
import "server-only";

function positiveInt(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isInteger(n) && n > 0 ? n : fallback;
}

/** "24h" -> 86400. Accepts whole seconds, minutes or hours, like the backend. */
function durationSeconds(value: string): number | null {
  const match = /^(\d+)([smh])$/.exec(value.trim());
  if (!match) return null;
  return Number(match[1]) * { s: 1, m: 60, h: 3600 }[match[2] as "s" | "m" | "h"];
}

/** A comma-separated list of durations; invalid entries are dropped. */
function durationList(value: string | undefined, fallback: string): number[] {
  const parse = (v: string) =>
    v
      .split(",")
      .map(durationSeconds)
      .filter((n): n is number => n !== null && n > 0);
  const parsed = parse(value ?? "");
  return parsed.length > 0 ? parsed : parse(fallback);
}

export const config = {
  backendUrl: process.env.BACKEND_URL ?? "http://localhost:8080",
  /**
   * The address shown to jobs inside the cluster for their push reports;
   * the backend's, unless that is not how other namespaces reach it.
   */
  pushInternalUrl: process.env.PUSH_INTERNAL_URL || process.env.BACKEND_URL || "http://localhost:8080",
  /** Shared secret sent to the backend; must match its UPTIME_API_TOKEN. */
  backendToken: process.env.BACKEND_TOKEN ?? "",
  backendTimeoutMs: positiveInt(process.env.BACKEND_TIMEOUT_MS, 3000),
  /** How often open pages reload their data. */
  refreshIntervalMs: positiveInt(process.env.REFRESH_INTERVAL_MS, 5000),
  /** How often an open status page reloads; visitors are many, so slower. */
  statusPageRefreshMs: positiveInt(process.env.STATUS_PAGE_REFRESH_MS, 30000),
  /** Number of recent checks drawn in a monitor's heartbeat bar. */
  historyLimit: positiveInt(process.env.HISTORY_LIMIT, 60),
  /** Number of recent checks drawn next to each monitor in the list. */
  listBeats: positiveInt(process.env.LIST_BEATS, 14),
  /**
   * Up to this many monitors the list shows every namespace opened; above
   * it, namespaces start closed unless something in them is failing.
   */
  dashboardFlatLimit: positiveInt(process.env.DASHBOARD_FLAT_LIMIT, 50),
  /** Number of recent events listed on the overview. */
  overviewEvents: positiveInt(process.env.OVERVIEW_EVENTS, 12),
  /** Number of certificates listed on the overview, soonest to expire first. */
  overviewCertificates: positiveInt(process.env.OVERVIEW_CERTIFICATES, 5),
  /** What the form of a new monitor starts with. */
  newMonitor: {
    interval: positiveInt(process.env.NEW_MONITOR_INTERVAL_SECONDS, 60),
    timeout: positiveInt(process.env.NEW_MONITOR_TIMEOUT_SECONDS, 10),
    failureThreshold: positiveInt(process.env.NEW_MONITOR_FAILURE_THRESHOLD, 3),
  },
  /** Number of recent checks listed on a monitor's page. */
  resultsLimit: positiveInt(process.env.RESULTS_LIMIT, 30),
  /** Periods for which a monitor's uptime is shown, in seconds. */
  uptimeWindows: durationList(process.env.UPTIME_WINDOWS, "24h,168h,720h"),
  /** Ranges offered for a monitor's charts, in seconds; the first is the default. */
  chartRanges: durationList(process.env.CHART_RANGES, "24h,1h,168h,720h"),
  /** Lengths offered when silencing a monitor or planning maintenance, in seconds. */
  maintenanceDurations: durationList(process.env.MAINTENANCE_DURATIONS, "30m,1h,2h,4h,8h,24h"),
  /** Number of maintenance windows listed. */
  maintenanceLimit: positiveInt(process.env.MAINTENANCE_LIMIT, 50),
  /** Number of incidents listed on a page. */
  incidentsLimit: positiveInt(process.env.INCIDENTS_LIMIT, 100),
};
