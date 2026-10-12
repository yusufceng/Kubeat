// Server-side settings of the frontend, read from the environment.

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
  backendTimeoutMs: positiveInt(process.env.BACKEND_TIMEOUT_MS, 3000),
  /** How often open pages reload their data. */
  refreshIntervalMs: positiveInt(process.env.REFRESH_INTERVAL_MS, 5000),
  /** Number of recent checks drawn in a history strip. */
  historyLimit: positiveInt(process.env.HISTORY_LIMIT, 40),
  /** Number of recent checks listed on a monitor's page. */
  resultsLimit: positiveInt(process.env.RESULTS_LIMIT, 100),
  /** Periods for which a monitor's uptime is shown, in seconds. */
  uptimeWindows: durationList(process.env.UPTIME_WINDOWS, "24h,168h,720h"),
  /** Ranges offered for a monitor's charts, in seconds; the first is the default. */
  chartRanges: durationList(process.env.CHART_RANGES, "24h,1h,168h,720h"),
  /** Number of incidents listed on a page. */
  incidentsLimit: positiveInt(process.env.INCIDENTS_LIMIT, 100),
};
