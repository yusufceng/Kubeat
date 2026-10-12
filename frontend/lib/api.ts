import { config } from "@/lib/config";

/**
 * The backend API revision this frontend was written against; see Revision in
 * the backend's api package. Raise both together.
 */
export const REQUIRED_API_REVISION = 1;

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
};

export type MonitorType = "http" | "tcp";

export type Incident = {
  id: number;
  monitor_id: number;
  monitor_name: string;
  started_at: string;
  /** null while the incident is open */
  resolved_at: string | null;
  cause: string;
};

export type Channel = {
  id: number;
  name: string;
  type: "webhook";
  /** masked by the backend */
  url: string;
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
  status: "up" | "down" | "pending";
  last_result: CheckResult | null;
  open_incident: Incident | null;
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
    open_incident: m.open_incident ?? null,
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

export const deleteMonitor = (id: number) =>
  request<void>(`/monitors/${id}`, { method: "DELETE" });

export const listIncidents = (limit: number, monitorId?: number) =>
  request<Incident[]>(`/incidents?limit=${limit}${monitorId ? `&monitor_id=${monitorId}` : ""}`);

export const listChannels = () => request<Channel[]>("/channels");

export const createChannel = (channel: { name: string; url: string }) =>
  request<Channel>("/channels", {
    method: "POST",
    body: JSON.stringify({ ...channel, type: "webhook" }),
  });

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
