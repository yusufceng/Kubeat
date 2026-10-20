/*
 * What the monitor page works out from the API's answers: names as kubectl
 * says them, the current streak, outages within a period, percents split
 * for typography, the maintenance a chart should mark, and where the time
 * of a failed check went. No imports but types, so that the tests run it as
 * it is.
 */
import type { DiagnosticTiming, Incident, Maintenance, Monitor, MonitorType } from "@/lib/api";

/** kubectl's short names of the kinds Kubeat discovers. */
const shortKinds: Record<string, string> = {
  service: "svc",
  route: "route",
  ingress: "ing",
  deployment: "deploy",
  statefulset: "sts",
  daemonset: "ds",
};

/** A discovered object as kubectl names it: "service/pricing:8080" is "svc/pricing:8080"; null for a monitor entered by hand. */
export function kubectlName(source: string): string | null {
  const slash = source.indexOf("/");
  if (slash <= 0) return null;
  const kind = source.slice(0, slash);
  return `${shortKinds[kind] ?? kind}/${source.slice(slash + 1)}`;
}

/** The check types as short lowercase words, for mono tags. */
export const typeWords: Record<MonitorType, string> = {
  http: "http",
  tcp: "tcp",
  ping: "ping",
  dns: "dns",
  push: "push",
  grpc: "grpc",
  websocket: "ws",
  postgres: "postgres",
  mysql: "mysql",
  redis: "redis",
  workload: "workload",
};

/**
 * When the monitor's present good run began: the end of its latest outage,
 * or else when it was created. incidents may be in any order.
 */
export function upSince(monitor: Pick<Monitor, "created_at">, incidents: Pick<Incident, "kind" | "resolved_at">[]): string {
  let latest: string | null = null;
  for (const i of incidents) {
    if (i.kind !== "down" || !i.resolved_at) continue;
    if (latest === null || Date.parse(i.resolved_at) > Date.parse(latest)) latest = i.resolved_at;
  }
  return latest ?? monitor.created_at;
}

/**
 * The outages that overlap [from, to) and how long the monitor was down
 * within it, in milliseconds; an outage still going on counts up to `to`.
 */
export function outagesWithin(
  incidents: Pick<Incident, "kind" | "started_at" | "resolved_at">[],
  from: number,
  to: number,
): { count: number; downMs: number } {
  let count = 0;
  let downMs = 0;
  for (const i of incidents) {
    if (i.kind !== "down") continue;
    const start = Date.parse(i.started_at);
    const end = i.resolved_at ? Date.parse(i.resolved_at) : to;
    if (end <= from || start >= to) continue;
    count++;
    downMs += Math.min(end, to) - Math.max(start, from);
  }
  return { count, downMs };
}

/** How an uptime reads: amber below 99.9%, red below 99%, plain otherwise. */
export function uptimeTone(percent: number | null): "down" | "warn" | null {
  if (percent === null) return null;
  if (percent < 99) return "down";
  if (percent < 99.9) return "warn";
  return null;
}

/**
 * A percent as the locale writes it, split so that the sign can be set
 * apart: { sign: "%", number: "99,55", signFirst: true } in Turkish,
 * { sign: "%", number: "99.55", signFirst: false } in English. It is
 * floored, so that 99.996% does not pass for a clean 100%; a whole 100
 * has no decimals.
 */
export function percentParts(percent: number, intl: string): { sign: string; number: string; signFirst: boolean } {
  const floored = Math.floor(percent * 100) / 100;
  const digits = floored === 100 ? 0 : 2;
  const parts = new Intl.NumberFormat(intl, {
    style: "percent",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).formatToParts(floored / 100);
  const at = parts.findIndex((p) => p.type === "percentSign");
  const number = parts
    .filter((p) => p.type !== "percentSign" && p.type !== "literal")
    .map((p) => p.value)
    .join("");
  return { sign: at >= 0 ? parts[at].value : "%", number, signFirst: at === 0 };
}

/** A stretch of time in Unix milliseconds; to is null while it goes on. */
export type Span = { from: number; to: number | null };

/**
 * The maintenance a monitor's chart can mark: the windows that cover it
 * (it alone, its namespace, or everything). A repeating window is known by
 * its current or next occurrence only; the API does not tell when it
 * began, so earlier occurrences are not guessed.
 */
export function maintenanceSpans(
  windows: Pick<Maintenance, "starts_at" | "ends_at" | "namespace" | "monitor_id">[],
  monitor: Pick<Monitor, "id" | "namespace">,
): { from: number; to: number }[] {
  return windows
    .filter((w) =>
      w.monitor_id !== null && w.monitor_id !== 0
        ? w.monitor_id === monitor.id
        : w.namespace !== ""
          ? w.namespace === monitor.namespace
          : true,
    )
    .map((w) => ({ from: Date.parse(w.starts_at), to: Date.parse(w.ends_at) }))
    .filter((s) => s.to > s.from)
    .sort((a, b) => a.from - b.from);
}

/**
 * One step of a check's time, in ms from the start of the request. A step
 * that did not happen is "skipped" when a later one did (an IP address
 * needs no lookup, a reused connection no connect or handshake, plain HTTP
 * no TLS) and "unreached" when the check failed before it.
 */
export type Phase =
  | { key: "dns" | "connect" | "tls" | "firstByte"; state: "done"; from: number; to: number }
  | { key: "dns" | "connect" | "tls" | "firstByte"; state: "skipped" | "unreached" };

/**
 * Where the time of an HTTP(S) or WebSocket check went, as a waterfall:
 * the lookup, the connection, the TLS handshake and the wait for the first
 * byte follow one another. first_byte_ms counts from the start of the
 * request, so the wait is what remains of it after the other steps. When
 * no answer came, `unanswered` is the time from the last step to the end,
 * spent waiting in vain.
 */
export function waterfall(timing: DiagnosticTiming): {
  phases: Phase[];
  total: number;
  unanswered: { from: number; to: number } | null;
} {
  const lengths = [timing.dns_ms, timing.connect_ms, timing.tls_ms];
  const keys = ["dns", "connect", "tls"] as const;
  const firstByte = timing.first_byte_ms;
  let at = 0;
  const phases: Phase[] = keys.map((key, i) => {
    const ms = lengths[i];
    if (ms === null) {
      const later = lengths.slice(i + 1).some((l) => l !== null) || firstByte !== null;
      return { key, state: later ? "skipped" : "unreached" };
    }
    const phase: Phase = { key, state: "done", from: at, to: at + ms };
    at += ms;
    return phase;
  });
  // A first byte before the end of the steps above means they overlapped
  // the clock's resolution; the wait is then nil, never negative.
  phases.push(
    firstByte === null
      ? { key: "firstByte", state: "unreached" }
      : { key: "firstByte", state: "done", from: Math.min(at, firstByte), to: firstByte },
  );
  const total = Math.max(timing.total_ms, firstByte ?? 0, at);
  return { phases, total, unanswered: firstByte === null ? { from: at, to: total } : null };
}

/**
 * A step's length as the waterfall writes it: milliseconds, with a decimal
 * below 10; from a second on, seconds with one decimal.
 */
export function stepLength(ms: number, intl: string): { value: string; unit: "ms" | "s" } {
  if (ms >= 1000) return { value: (ms / 1000).toLocaleString(intl, { maximumFractionDigits: 1 }), unit: "s" };
  return { value: ms.toLocaleString(intl, { maximumFractionDigits: ms < 10 ? 1 : 0 }), unit: "ms" };
}
