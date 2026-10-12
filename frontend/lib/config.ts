// Server-side settings of the frontend, read from the environment.

function positiveInt(value: string | undefined, fallback: number): number {
  const n = Number(value);
  return Number.isInteger(n) && n > 0 ? n : fallback;
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
};
