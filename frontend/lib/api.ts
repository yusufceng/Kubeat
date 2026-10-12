import { config } from "@/lib/config";

/**
 * The backend API revision this frontend was written against; see Revision in
 * the backend's api package. Raise both together.
 */
export const REQUIRED_API_REVISION = 6;

export type Health = {
  status: string;
  version: string;
  /** Missing on backends older than the revision scheme itself. */
  api_revision?: number;
  time: string;
};

export type CheckResult = {
  checked_at: string;
  up: boolean;
  latency_ms: number;
  status_code: number | null;
  error: string | null;
  /** When the target's TLS certificate expires; null if the check saw none. */
  cert_expires_at: string | null;
};

export type MonitorType = "http" | "tcp";

/** Shown in place of a stored header value; sent back, it keeps that value. */
export const MASKED_VALUE = "********";

export type HttpOptions = {
  method: "GET" | "HEAD";
  /** Accepted status codes: codes and ranges such as "200-299,401". */
  expected_status: string;
  /** Text the body must contain; empty when the body is not inspected. */
  keyword: string;
  /** Values come back masked. */
  headers: Record<string, string>;
  tls_skip_verify: boolean;
};

export type Incident = {
  id: number;
  monitor_id: number;
  monitor_name: string;
  /** An outage, or a certificate close to its expiry. */
  kind: "down" | "cert";
  started_at: string;
  /** null while the incident is open */
  resolved_at: string | null;
  cause: string;
};

export type ChannelType = "webhook" | "slack" | "teams" | "email";

export type Channel = {
  id: number;
  name: string;
  type: ChannelType;
  /** A URL, masked by the backend, or for e-mail the list of addresses. */
  target: string;
  enabled: boolean;
  created_at: string;
};

export type ChannelTest = { delivered: boolean; error?: string };

export type Monitor = {
  id: number;
  name: string;
  type: MonitorType;
  target: string;
  /** Empty for monitors entered by hand. */
  namespace: string;
  /** The discovered object, e.g. "service/api:80"; empty for manual monitors. */
  source: string;
  interval_seconds: number;
  timeout_seconds: number;
  failure_threshold: number;
  enabled: boolean;
  created_at: string;
  /** null for monitors that are not HTTP checks. */
  http: HttpOptions | null;
  status: "up" | "down" | "pending" | "paused";
  last_result: CheckResult | null;
  open_incident: Incident | null;
  /** True while the target's certificate is close to its expiry. */
  cert_warning: boolean;
  window_seconds: number;
  checks: number;
  uptime_percent: number | null;
  avg_latency_ms: number;
};

export type NewMonitor = Pick<
  Monitor,
  "name" | "type" | "target" | "interval_seconds" | "timeout_seconds" | "failure_threshold"
>;

/** A non-2xx answer from the backend. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${config.backendUrl}/api/v1${path}`, {
    ...init,
    signal: AbortSignal.timeout(config.backendTimeoutMs),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new ApiError(res.status, body?.error ?? `backend responded with ${res.status}`);
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

export const getHealth = () => request<Health>("/health");

/**
 * Fills in fields an older backend does not send yet, so that a version
 * mismatch degrades the page instead of crashing it. The mismatch itself is
 * reported by the layout's backend warning.
 */
function withDefaults(m: Monitor): Monitor {
  return {
    ...m,
    namespace: m.namespace ?? "",
    source: m.source ?? "",
    failure_threshold: m.failure_threshold ?? 0,
    http: m.http ?? null,
    open_incident: m.open_incident ?? null,
    cert_warning: m.cert_warning ?? false,
  };
}

export const listMonitors = async () =>
  (await request<Monitor[]>("/monitors")).map(withDefaults);

export const getMonitor = async (id: number) =>
  withDefaults(await request<Monitor>(`/monitors/${id}`));

export const listResults = (id: number, limit: number) =>
  request<CheckResult[]>(`/monitors/${id}/results?limit=${limit}`);

export const createMonitor = (monitor: NewMonitor) =>
  request<Monitor>("/monitors", { method: "POST", body: JSON.stringify(monitor) });

export type MonitorChanges = Partial<
  Pick<
    Monitor,
    "name" | "target" | "interval_seconds" | "timeout_seconds" | "failure_threshold" | "enabled"
  >
> & { http?: HttpOptions };

export const updateMonitor = (id: number, changes: MonitorChanges) =>
  request<Monitor>(`/monitors/${id}`, { method: "PATCH", body: JSON.stringify(changes) });

export const deleteMonitor = (id: number) =>
  request<void>(`/monitors/${id}`, { method: "DELETE" });

export const listIncidents = (limit: number, monitorId?: number) =>
  request<Incident[]>(`/incidents?limit=${limit}${monitorId ? `&monitor_id=${monitorId}` : ""}`);

export const listChannels = () => request<Channel[]>("/channels");

export const createChannel = (channel: { name: string; type: ChannelType; target: string }) =>
  request<Channel>("/channels", { method: "POST", body: JSON.stringify(channel) });

export const deleteChannel = (id: number) =>
  request<void>(`/channels/${id}`, { method: "DELETE" });

export const testChannel = (id: number) =>
  request<ChannelTest>(`/channels/${id}/test`, { method: "POST" });

export type DiscoveryStatus = { enabled: boolean; detail: string };

export type Candidate = {
  kind: "service" | "ingress" | "route";
  namespace: string;
  name: string;
  port: number;
  source: string;
  type: MonitorType;
  target: string;
  reason: "readiness-probe" | "liveness-probe" | "tcp-fallback" | "route-host" | "ingress-rule";
  /** Set when the candidate is already monitored. */
  monitor_id: number | null;
};

export type ImportResult = {
  created: Monitor[];
  skipped: { source: string; reason: string }[];
};

export const getDiscoveryStatus = () => request<DiscoveryStatus>("/discovery");

export const listNamespaces = () => request<string[]>("/discovery/namespaces");

export const listCandidates = (namespace: string) =>
  request<Candidate[]>(`/discovery/candidates?namespace=${encodeURIComponent(namespace)}`);

export const importCandidates = (body: {
  namespace: string;
  sources: string[];
  interval_seconds: number;
  timeout_seconds: number;
  failure_threshold: number;
}) => request<ImportResult>("/discovery/import", { method: "POST", body: JSON.stringify(body) });

export type SeriesPoint = {
  start: string;
  checks: number;
  up_checks: number;
  /** Successful checks only; null when there were none in the bucket. */
  avg_latency_ms: number | null;
  max_latency_ms: number | null;
};

export type Series = {
  range_seconds: number;
  bucket_seconds: number;
  from: string;
  to: string;
  /** Oldest first; buckets without checks are left out. */
  points: SeriesPoint[];
};

export type UptimeWindow = {
  window_seconds: number;
  checks: number;
  up_checks: number;
  uptime_percent: number | null;
};

export const getSeries = (id: number, rangeSeconds: number) =>
  request<Series>(`/monitors/${id}/series?range=${rangeSeconds}s`);

export const getUptime = (id: number, windowsSeconds: number[]) =>
  request<UptimeWindow[]>(
    `/monitors/${id}/uptime?windows=${windowsSeconds.map((s) => `${s}s`).join(",")}`,
  );
