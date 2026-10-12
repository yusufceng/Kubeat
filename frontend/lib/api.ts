import { config } from "@/lib/config";

export type Health = {
  status: string;
  version: string;
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

export type Monitor = {
  id: number;
  name: string;
  type: MonitorType;
  target: string;
  interval_seconds: number;
  timeout_seconds: number;
  enabled: boolean;
  created_at: string;
  status: "up" | "down" | "pending";
  last_result: CheckResult | null;
  window_seconds: number;
  checks: number;
  uptime_percent: number | null;
  avg_latency_ms: number;
};

export type NewMonitor = Pick<
  Monitor,
  "name" | "type" | "target" | "interval_seconds" | "timeout_seconds"
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

export const listMonitors = () => request<Monitor[]>("/monitors");

export const getMonitor = (id: number) => request<Monitor>(`/monitors/${id}`);

export const listResults = (id: number, limit: number) =>
  request<CheckResult[]>(`/monitors/${id}/results?limit=${limit}`);

export const createMonitor = (monitor: NewMonitor) =>
  request<Monitor>("/monitors", { method: "POST", body: JSON.stringify(monitor) });

export const deleteMonitor = (id: number) =>
  request<void>(`/monitors/${id}`, { method: "DELETE" });
