import "server-only";

import { requireSession } from "@/lib/auth";
import { config } from "@/lib/config";

/**
 * The backend API revision this frontend was written against; see Revision in
 * the backend's api package. Raise both together.
 */
export const REQUIRED_API_REVISION = 12;

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
  /** What a successful check found, when there is something to say. */
  detail: string | null;
  /** When the target's TLS certificate expires; null if the check saw none. */
  cert_expires_at: string | null;
};

export type MonitorType = "http" | "tcp" | "ping" | "dns" | "push" | "grpc" | "websocket";

/** One past check in the compact form the monitor list uses. */
export type HistoryPoint = {
  /** When it ran, Unix milliseconds. */
  t: number;
  up: boolean;
  /** How long it took, milliseconds. */
  ms: number;
  error?: string;
};

export type HttpMethod = "GET" | "HEAD" | "POST" | "PUT" | "PATCH" | "DELETE" | "OPTIONS";

export type JsonOperator = "==" | "!=" | ">" | ">=" | "<" | "<=" | "contains";

export type HttpOptions = {
  method: HttpMethod;
  /** Accepted status codes: codes and ranges such as "200-299,401". */
  expected_status: string;
  /** Text the body must contain, or with invert_keyword must not; empty when not checked. */
  keyword: string;
  invert_keyword: boolean;
  /** A value in a JSON response, such as "checks[0].state"; empty when not checked. */
  json_path: string;
  json_operator: JsonOperator | "";
  json_expected: string;
  /** Values come back masked. */
  headers: Record<string, string>;
  body: string;
  content_type: string;
  auth: "" | "basic" | "bearer";
  auth_user: string;
  /** The password or token; comes back masked. */
  auth_secret: string;
  /** How many redirects are followed; 0 judges the first response. */
  max_redirects: number;
  tls_skip_verify: boolean;
};

export type TcpOptions = {
  /** Complete a TLS handshake, which also reads the certificate's expiry. */
  tls: boolean;
  tls_skip_verify: boolean;
};

export type DnsRecordType = "A" | "AAAA" | "CNAME" | "MX" | "NS" | "TXT" | "SRV" | "PTR";

export type DnsOptions = {
  record_type: DnsRecordType;
  /** The server to ask, host or host:port; empty for the system resolver. */
  resolver: string;
  /** Text one of the records must contain; empty when any record will do. */
  expected: string;
};

export type GrpcOptions = {
  /** The service asked about; empty for the server as a whole. */
  service: string;
  tls: boolean;
  tls_skip_verify: boolean;
};

export type WebSocketOptions = {
  subprotocol: string;
  tls_skip_verify: boolean;
};

/** The check options of a monitor: only the one of its own type is set. */
export type CheckOptions = {
  http: HttpOptions | null;
  tcp: TcpOptions | null;
  dns: DnsOptions | null;
  grpc: GrpcOptions | null;
  websocket: WebSocketOptions | null;
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

export type ChannelType =
  | "webhook"
  | "slack"
  | "teams"
  | "email"
  | "discord"
  | "mattermost"
  | "googlechat"
  | "telegram"
  | "pagerduty"
  | "ntfy"
  | "gotify";

export type Channel = {
  id: number;
  name: string;
  type: ChannelType;
  /** Where it sends to; URLs come back masked. Empty for types without one. */
  target: string;
  /** Whether a token or key is stored; its value never leaves the backend. */
  has_secret: boolean;
  /** The namespaces whose monitors it hears about; empty for all of them. */
  namespaces: string[];
  enabled: boolean;
  created_at: string;
};

/** What a client sets on a channel. */
export type ChannelSettings = {
  name: string;
  /** Sent back as it was shown, it keeps the stored target. */
  target: string;
  /** Sent as the mask, it keeps the stored secret. */
  secret: string;
  namespaces: string[];
};

export type ChannelTest = { delivered: boolean; error?: string };

export type Monitor = CheckOptions & {
  id: number;
  name: string;
  description: string;
  type: MonitorType;
  /** Empty for push monitors. */
  target: string;
  /** Empty for monitors entered by hand. */
  namespace: string;
  /** The discovered object, e.g. "service/api:80"; empty for manual monitors. */
  source: string;
  interval_seconds: number;
  timeout_seconds: number;
  /** How soon a failed check is repeated before the outage is confirmed; 0 for the interval. */
  retry_interval_seconds: number;
  failure_threshold: number;
  /** Up while the target fails. */
  inverted: boolean;
  enabled: boolean;
  created_at: string;
  /** The secret of a push monitor's report URL; absent for other types. */
  push_token?: string;
  status: "up" | "down" | "pending" | "paused";
  last_result: CheckResult | null;
  open_incident: Incident | null;
  /** True while the target's certificate is close to its expiry. */
  cert_warning: boolean;
  /** Set while a maintenance window covers the monitor. */
  maintenance_until: string | null;
  /** Recent checks, newest first; empty unless the list was asked for them. */
  history: HistoryPoint[];
  window_seconds: number;
  checks: number;
  uptime_percent: number | null;
  avg_latency_ms: number;
};

/**
 * What a client sets on a monitor. Options that are left out take the
 * backend's defaults; the options of the monitor's own type, when given,
 * replace all of them.
 */
export type MonitorSettings = {
  name: string;
  target: string;
  interval_seconds: number;
  timeout_seconds: number;
  failure_threshold: number;
  description?: string;
  retry_interval_seconds?: number;
  inverted?: boolean;
  http?: Partial<HttpOptions>;
  tcp?: Partial<TcpOptions>;
  dns?: Partial<DnsOptions>;
  grpc?: Partial<GrpcOptions>;
  websocket?: Partial<WebSocketOptions>;
};

export type NewMonitor = MonitorSettings & { type: MonitorType };

/** A non-2xx answer from the backend. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(
  path: string,
  init?: RequestInit,
  /** Skip the session check; only for what is public anyway. */
  { open = false }: { open?: boolean } = {},
): Promise<T> {
  if (!open) await requireSession();
  const res = await fetch(`${config.backendUrl}/api/v1${path}`, {
    ...init,
    headers: {
      ...init?.headers,
      ...(config.backendToken && { Authorization: `Bearer ${config.backendToken}` }),
    },
    signal: AbortSignal.timeout(config.backendTimeoutMs),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    throw new ApiError(res.status, body?.error ?? `backend responded with ${res.status}`);
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

// Also shown on the sign-in page, and the backend serves it to anyone.
export const getHealth = () => request<Health>("/health", undefined, { open: true });

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
    description: m.description ?? "",
    retry_interval_seconds: m.retry_interval_seconds ?? 0,
    inverted: m.inverted ?? false,
    http: m.http ?? null,
    tcp: m.tcp ?? null,
    dns: m.dns ?? null,
    grpc: m.grpc ?? null,
    websocket: m.websocket ?? null,
    open_incident: m.open_incident ?? null,
    cert_warning: m.cert_warning ?? false,
    maintenance_until: m.maintenance_until ?? null,
    history: m.history ?? [],
  };
}

export type MonitorFilter = {
  /** Only this namespace; "" selects monitors without one. */
  namespace?: string;
  ids?: number[];
  /** Include this many recent checks per monitor. */
  history?: number;
};

export const listMonitors = async (filter: MonitorFilter = {}) => {
  const params = new URLSearchParams();
  if (filter.namespace !== undefined) params.set("namespace", filter.namespace);
  if (filter.ids) params.set("ids", filter.ids.join(","));
  if (filter.history) params.set("history", String(filter.history));
  const query = params.size > 0 ? `?${params}` : "";
  return (await request<Monitor[]>(`/monitors${query}`)).map(withDefaults);
};

export const getMonitor = async (id: number) =>
  withDefaults(await request<Monitor>(`/monitors/${id}`));

export const listResults = (id: number, limit: number) =>
  request<CheckResult[]>(`/monitors/${id}/results?limit=${limit}`);

export const createMonitor = (monitor: NewMonitor) =>
  request<Monitor>("/monitors", { method: "POST", body: JSON.stringify(monitor) });

export type MonitorChanges = Partial<MonitorSettings & { enabled: boolean }>;

/** A copy of the monitor, secrets included, under a new name. */
export const cloneMonitor = (id: number, name: string) =>
  request<Monitor>(`/monitors/${id}/clone`, { method: "POST", body: JSON.stringify({ name }) });

export const updateMonitor = (id: number, changes: MonitorChanges) =>
  request<Monitor>(`/monitors/${id}`, { method: "PATCH", body: JSON.stringify(changes) });

export const deleteMonitor = (id: number) =>
  request<void>(`/monitors/${id}`, { method: "DELETE" });

export const listIncidents = (limit: number, monitorId?: number) =>
  request<Incident[]>(`/incidents?limit=${limit}${monitorId ? `&monitor_id=${monitorId}` : ""}`);

export const listChannels = () => request<Channel[]>("/channels");

export const createChannel = (channel: ChannelSettings & { type: ChannelType }) =>
  request<Channel>("/channels", { method: "POST", body: JSON.stringify(channel) });

export const updateChannel = (id: number, changes: Partial<ChannelSettings & { enabled: boolean }>) =>
  request<Channel>(`/channels/${id}`, { method: "PATCH", body: JSON.stringify(changes) });

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

export type Maintenance = {
  id: number;
  name: string;
  starts_at: string;
  ends_at: string;
  state: "active" | "upcoming" | "past";
  /** Scope: one monitor, one namespace, or (both empty) every monitor. */
  namespace: string;
  monitor_id: number | null;
  monitor_name: string;
};

export type NewMaintenance = {
  name: string;
  /** ISO time; omitted to start now. */
  starts_at?: string;
  duration_seconds: number;
  namespace?: string;
  monitor_id?: number;
};

export const listMaintenance = (limit: number) =>
  request<Maintenance[]>(`/maintenance?limit=${limit}`);

export const createMaintenance = (window: NewMaintenance) =>
  request<Maintenance>("/maintenance", { method: "POST", body: JSON.stringify(window) });

export const deleteMaintenance = (id: number) =>
  request<void>(`/maintenance/${id}`, { method: "DELETE" });

export type Pod = {
  name: string;
  /** Whether the pod receives traffic. */
  ready: boolean;
  ready_containers: number;
  containers: number;
  restarts: number;
  /** The phase, or the reason the pod is not running normally. */
  status: string;
  started_at: string | null;
  node: string;
  /** null when the metrics server has no value for the pod. */
  cpu_cores: number | null;
  memory_bytes: number | null;
};

export type Workload = {
  /** False when there are no pods to show; reason says why. */
  available: boolean;
  reason?: string;
  service?: string;
  ready: number;
  total: number;
  pods: Pod[];
  metrics: { available: boolean; reason?: string };
};

export const getWorkload = (id: number) => request<Workload>(`/monitors/${id}/workload`);

export type StatusPageMonitor = {
  monitor_id: number;
  /** Shown instead of the monitor's own name; empty for that. */
  display_name: string;
};

export type StatusPage = {
  id: number;
  /** The last part of the page's address, /status/{slug}. */
  slug: string;
  title: string;
  description: string;
  /** A notice at the top of the page; empty for none. */
  announcement: string;
  /** An unpublished page is not served. */
  published: boolean;
  /** In the order the page shows them. */
  monitors: StatusPageMonitor[];
  created_at: string;
};

export type StatusPageSettings = Omit<StatusPage, "id" | "created_at">;

export const listStatusPages = () => request<StatusPage[]>("/status-pages");

export const createStatusPage = (page: StatusPageSettings) =>
  request<StatusPage>("/status-pages", { method: "POST", body: JSON.stringify(page) });

export const updateStatusPage = (id: number, changes: Partial<StatusPageSettings>) =>
  request<StatusPage>(`/status-pages/${id}`, { method: "PATCH", body: JSON.stringify(changes) });

export const deleteStatusPage = (id: number) =>
  request<void>(`/status-pages/${id}`, { method: "DELETE" });

/** What a status page tells the public about a monitor. */
export type PublicState = "up" | "down" | "maintenance" | "pending" | "paused";

export type StatusPageView = {
  slug: string;
  title: string;
  description: string;
  announcement: string;
  /** The page as a whole. */
  state: "up" | "degraded" | "down" | "maintenance";
  /** How many days each history covers, ending today (UTC). */
  days: number;
  /** The start of the first of those days. */
  from: string;
  monitors: {
    id: number;
    name: string;
    state: PublicState;
    uptime_percent: number | null;
    /** One entry per day, oldest first; null for a day without checks. */
    history: (number | null)[];
  }[];
  /** Outages, newest first. */
  incidents: { monitor_name: string; started_at: string; resolved_at: string | null }[];
};

/**
 * What a published status page shows. It is read without a session: the
 * page is public, and the backend puts nothing internal in it.
 */
export const getStatusPageView = (slug: string) =>
  request<StatusPageView>(`/status-pages/by-slug/${encodeURIComponent(slug)}/view`, undefined, {
    open: true,
  });
