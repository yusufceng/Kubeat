import type { Monitor } from "@/lib/api";

const kindLabels: Record<string, string> = {
  route: "Route",
  ingress: "Ingress",
  service: "Service",
  deployment: "Deployment",
  statefulset: "StatefulSet",
  daemonset: "DaemonSet",
};

type Named = Pick<Monitor, "name" | "source" | "namespace">;

/**
 * The name a monitor goes by on screen, and for a discovered one its kind
 * ("Service", "Route", ...). It is the monitor's own name, the one its
 * alerts and incidents carry. Only the name discovery gives, which is
 * where the monitor came from ("checkout/service/pricing:8080"), is shown
 * as the object's own name ("pricing:8080"): the namespace and the kind
 * stand beside it already. derived says the name is that object's.
 */
export function monitorLabel(m: Named): { kind: string | null; name: string; derived: boolean } {
  const slash = m.source.indexOf("/");
  if (slash < 0) return { kind: null, name: m.name, derived: false };
  const kind = m.source.slice(0, slash);
  const derived = m.name === `${m.namespace}/${m.source}`;
  return { kind: kindLabels[kind] ?? kind, name: derived ? m.source.slice(slash + 1) : m.name, derived };
}

/**
 * The names a monitor is known by besides the one on screen, for finding
 * it: the name of its alerts when that is longer, and the object it
 * follows ("service/pricing:8080"). Empty for a monitor entered by hand.
 */
export function otherNames(m: Named): string {
  const shown = monitorLabel(m).name;
  return [m.name === shown ? "" : m.name, m.source].filter(Boolean).join(" ");
}

export function KindChip({ kind }: { kind: string }) {
  return (
    <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal">
      {kind}
    </span>
  );
}
