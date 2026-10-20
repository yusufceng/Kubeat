// Server-side settings of the frontend, read from the environment.
import "server-only";

function positiveInt(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isInteger(n) && n > 0 ? n : fallback;
}

/** A percentage above 0 and at most 100, such as 99.9. */
function percent(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return value?.trim() && Number.isFinite(n) && n > 0 && n <= 100 ? n : fallback;
}

/** positiveInt for settings where zero means "off". */
function nonNegativeInt(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return value?.trim() && Number.isInteger(n) && n >= 0 ? n : fallback;
}

/** "auto", "always" or "never"; anything else is "auto". */
function cookieSecureMode(value: string | undefined): "auto" | "always" | "never" {
  const mode = value?.trim().toLowerCase();
  return mode === "always" || mode === "never" ? mode : "auto";
}

/**
 * A CSP source list such as "'self' https://wiki.example.com". The keywords
 * none and self get their quotes when written without (a quoted 'none' is
 * hard to pass through YAML and shells); a list that could end the
 * directive and start another, or is not plain ASCII, falls back.
 */
function cspSources(value: string | undefined, fallback: string): string {
  const sources = (value ?? "")
    .split(/\s+/)
    .filter(Boolean)
    .map((s) => (s === "none" || s === "self" ? `'${s}'` : s));
  return sources.length > 0 && sources.every((s) => /^[\x21-\x7e]+$/.test(s) && !/[;,]/.test(s))
    ? sources.join(" ")
    : fallback;
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

/**
 * A comma-separated list of whole days from 1 to 365 (what the backend's
 * reports accept); invalid entries and repeats are dropped.
 */
function dayList(value: string | undefined, fallback: string): number[] {
  const parse = (v: string) => [
    ...new Set(
      v
        .split(",")
        .map((d) => Number(d.trim()))
        .filter((n) => Number.isInteger(n) && n >= 1 && n <= 365),
    ),
  ];
  const parsed = parse(value ?? "");
  return parsed.length > 0 ? parsed : parse(fallback);
}

export const config = {
  backendUrl: process.env.BACKEND_URL ?? "http://localhost:8080",
  /** The language for visitors whose browser asks for none we have: "en" or "tr". */
  defaultLocale: (process.env.DEFAULT_LOCALE === "tr" ? "tr" : "en") as "en" | "tr",
  /**
   * The address shown to jobs inside the cluster for their push reports;
   * the backend's, unless that is not how other namespaces reach it.
   */
  pushInternalUrl: process.env.PUSH_INTERNAL_URL || process.env.BACKEND_URL || "http://localhost:8080",
  backendTimeoutMs: positiveInt(process.env.BACKEND_TIMEOUT_MS, 3000),
  /**
   * Where users reach Kubeat, as "https://kubeat.apps.example": the
   * backend's UPTIME_PUBLIC_URL. When it is HTTPS, browsers are on HTTPS
   * whatever the proxies in front of us say (see secureCookies and the
   * Strict-Transport-Security header); when it is http://, they may be on
   * plain HTTP whatever the proxies say (see lib/security-headers.ts).
   */
  publicUrl: (process.env.PUBLIC_URL ?? "").trim().replace(/\/+$/, ""),
  /**
   * Whether cookies are marked Secure, sent over HTTPS only: "always",
   * "never", or "auto": when publicUrl is HTTPS or the request came over
   * HTTPS. "never" is for plain HTTP from anywhere but localhost, and also
   * keeps upgrade-insecure-requests out of the Content-Security-Policy.
   */
  cookieSecure: cookieSecureMode(process.env.COOKIE_SECURE),
  /**
   * The pages that may show a status page in a frame (a wiki, a wall
   * display), as a CSP frame-ancestors source list such as
   * "https://wiki.example.com"; "'none'" allows none. The rest of the UI
   * is never shown in a frame.
   */
  statusPageFrameAncestors: cspSources(process.env.STATUS_PAGE_FRAME_ANCESTORS, "'none'"),
  /**
   * How long, in seconds, a browser that reached us over HTTPS keeps to
   * HTTPS for this host (Strict-Transport-Security); 0 sends no such header.
   */
  hstsMaxAge: nonNegativeInt(process.env.HSTS_MAX_AGE, 31536000),
  /** How often open pages reload their data. */
  refreshIntervalMs: positiveInt(process.env.REFRESH_INTERVAL_MS, 5000),
  /** How often an open status page reloads; visitors are many, so slower. */
  statusPageRefreshMs: positiveInt(process.env.STATUS_PAGE_REFRESH_MS, 30000),
  /** Number of recent checks drawn in a monitor's heartbeat bar. */
  historyLimit: positiveInt(process.env.HISTORY_LIMIT, 60),
  /** Number of recent checks drawn next to each monitor in the list. */
  listBeats: positiveInt(process.env.LIST_BEATS, 24),
  /**
   * Up to this many monitors the list shows every namespace opened; above
   * it, namespaces start closed unless something in them is failing.
   */
  dashboardFlatLimit: positiveInt(process.env.DASHBOARD_FLAT_LIMIT, 50),
  /** Number of recent events listed on the overview and a namespace's page. */
  overviewEvents: positiveInt(process.env.OVERVIEW_EVENTS, 12),
  /** Number of ongoing outages listed in the overview's summary; the rest are a click away in the list. */
  overviewOutages: positiveInt(process.env.OVERVIEW_OUTAGES, 5),
  /**
   * The fleet strip of the overview and of a namespace: how many hours it
   * covers, in how many slots (15 minutes each by default).
   */
  fleetStrip: {
    hours: positiveInt(process.env.FLEET_STRIP_HOURS, 24),
    slots: positiveInt(process.env.FLEET_STRIP_SLOTS, 96),
  },
  /** The period, in days, of the reliability figures (MTTR, MTBF, outages) on the overview and a namespace's page; at most 365. */
  statsDays: Math.min(365, positiveInt(process.env.STATS_DAYS, 7)),
  /**
   * Uptime below warnBelow percent is shown in amber, below criticalBelow
   * in red, in the monitor list and the tables; above, it stays quiet.
   */
  uptimeTones: {
    warnBelow: percent(process.env.WARN_UPTIME_BELOW, 99.9),
    criticalBelow: percent(process.env.CRITICAL_UPTIME_BELOW, 99),
  },
  /** What the form of a new monitor starts with. */
  newMonitor: {
    interval: positiveInt(process.env.NEW_MONITOR_INTERVAL_SECONDS, 60),
    timeout: positiveInt(process.env.NEW_MONITOR_TIMEOUT_SECONDS, 10),
    failureThreshold: positiveInt(process.env.NEW_MONITOR_FAILURE_THRESHOLD, 3),
  },
  /** Number of recent checks listed on a monitor's page. */
  resultsLimit: positiveInt(process.env.RESULTS_LIMIT, 30),
  /** Number of incidents a monitor's page lists before the rest wait behind "Show all". */
  monitorIncidents: positiveInt(process.env.MONITOR_INCIDENTS, 6),
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
  /** The periods the incident list offers to narrow it to, in seconds, up to now. */
  incidentRanges: durationList(process.env.INCIDENT_RANGES, "24h,168h,720h,2160h"),
  /** The periods the reports offer, in whole days; the first is the default. */
  reportPeriods: dayList(process.env.REPORT_PERIODS, "30,1,7,90"),
  /** Number of audit entries listed. */
  auditLimit: positiveInt(process.env.AUDIT_LIMIT, 200),
};
