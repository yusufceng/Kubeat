import type { MonitorType } from "@/lib/api";

/** How a monitor type is presented and what its target looks like. */
export type TypeInfo = {
  label: string;
  /** One line under the type selector. */
  summary: string;
  /** The label and an example of the target; null for types without one. */
  target: { label: string; placeholder: string } | null;
};

export const monitorTypes: Record<MonitorType, TypeInfo> = {
  http: {
    label: "HTTP(S)",
    summary: "Bir adrese istek atar; durum kodunu, gövdeyi ve sertifikayı denetler.",
    target: { label: "URL", placeholder: "https://api.example.com/healthz" },
  },
  tcp: {
    label: "TCP port",
    summary: "Bir porta bağlanır; istenirse TLS el sıkışması da yapar.",
    target: { label: "Adres", placeholder: "postgres.shop.svc:5432" },
  },
  ping: {
    label: "Ping",
    summary: "ICMP echo gönderir ve gidiş-dönüş süresini ölçer.",
    target: { label: "Sunucu", placeholder: "10.0.0.1 ya da node-1.example.com" },
  },
  dns: {
    label: "DNS",
    summary: "Bir adı çözer; kaydın varlığını ve değerini denetler.",
    target: { label: "Alan adı", placeholder: "api.shop.svc.cluster.local" },
  },
  push: {
    label: "Push",
    summary: "İzlenen iş kendisi rapor verir; rapor gelmezse alarm üretilir.",
    target: null,
  },
  grpc: {
    label: "gRPC",
    summary: "gRPC sağlık protokolüyle servisin SERVING olup olmadığını sorar.",
    target: { label: "Adres", placeholder: "cart.shop.svc:9090" },
  },
  websocket: {
    label: "WebSocket",
    summary: "Bağlantının WebSocket'e yükseltilebildiğini denetler.",
    target: { label: "URL", placeholder: "wss://live.example.com/socket" },
  },
};

/** The types in the order they are offered. */
export const monitorTypeOrder: MonitorType[] = ["http", "tcp", "ping", "dns", "grpc", "websocket", "push"];

export function typeLabel(type: MonitorType): string {
  return monitorTypes[type]?.label ?? type;
}
