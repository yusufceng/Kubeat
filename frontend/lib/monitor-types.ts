import type { MonitorType } from "@/lib/api";

/**
 * How a monitor type is presented and what its target looks like. Every
 * text here is a message key: show it with t().
 */
export type TypeInfo = {
  label: string;
  /** One line under the type selector. */
  summary: string;
  /** The label and an example of the target; null for types without one. */
  target: { label: string; placeholder: string } | null;
};

export const monitorTypes: Record<MonitorType, TypeInfo> = {
  http: {
    label: "monitorType.http",
    summary: "monitorType.http.summary",
    target: { label: "monitorType.http.target.label", placeholder: "monitorType.http.target.placeholder" },
  },
  tcp: {
    label: "monitorType.tcp",
    summary: "monitorType.tcp.summary",
    target: { label: "monitorType.tcp.target.label", placeholder: "monitorType.tcp.target.placeholder" },
  },
  ping: {
    label: "monitorType.ping",
    summary: "monitorType.ping.summary",
    target: { label: "monitorType.ping.target.label", placeholder: "monitorType.ping.target.placeholder" },
  },
  dns: {
    label: "monitorType.dns",
    summary: "monitorType.dns.summary",
    target: { label: "monitorType.dns.target.label", placeholder: "monitorType.dns.target.placeholder" },
  },
  push: {
    label: "monitorType.push",
    summary: "monitorType.push.summary",
    target: null,
  },
  grpc: {
    label: "monitorType.grpc",
    summary: "monitorType.grpc.summary",
    target: { label: "monitorType.grpc.target.label", placeholder: "monitorType.grpc.target.placeholder" },
  },
  postgres: {
    label: "monitorType.postgres",
    summary: "monitorType.postgres.summary",
    target: { label: "monitorType.postgres.target.label", placeholder: "monitorType.postgres.target.placeholder" },
  },
  mysql: {
    label: "monitorType.mysql",
    summary: "monitorType.mysql.summary",
    target: { label: "monitorType.mysql.target.label", placeholder: "monitorType.mysql.target.placeholder" },
  },
  redis: {
    label: "monitorType.redis",
    summary: "monitorType.redis.summary",
    target: { label: "monitorType.redis.target.label", placeholder: "monitorType.redis.target.placeholder" },
  },
  workload: {
    label: "monitorType.workload",
    summary: "monitorType.workload.summary",
    target: { label: "monitorType.workload.target.label", placeholder: "monitorType.workload.target.placeholder" },
  },
  websocket: {
    label: "monitorType.websocket",
    summary: "monitorType.websocket.summary",
    target: { label: "monitorType.websocket.target.label", placeholder: "monitorType.websocket.target.placeholder" },
  },
};

/** The types in the order they are offered. */
export const monitorTypeOrder: MonitorType[] = [
  "http",
  "tcp",
  "ping",
  "dns",
  "grpc",
  "websocket",
  "postgres",
  "mysql",
  "redis",
  "workload",
  "push",
];

/** The types whose check logs in to a database server. */
export function isDatabaseType(type: MonitorType): boolean {
  return type === "postgres" || type === "mysql" || type === "redis";
}

/** The message key of the type's name ("monitorType.http"); show it with t(). */
export function typeLabel(type: MonitorType): string {
  return monitorTypes[type]?.label ?? type;
}
