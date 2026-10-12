import type { Monitor } from "@/lib/api";

const kindLabels: Record<string, string> = {
  route: "Route",
  ingress: "Ingress",
  service: "Service",
};

/**
 * Splits a discovered monitor's source ("service/api:80") into the kind and
 * the object's own name; monitors entered by hand just have their name.
 */
export function monitorLabel(m: Pick<Monitor, "name" | "source">): {
  kind: string | null;
  name: string;
} {
  const slash = m.source.indexOf("/");
  if (slash < 0) return { kind: null, name: m.name };
  const kind = m.source.slice(0, slash);
  return { kind: kindLabels[kind] ?? kind, name: m.source.slice(slash + 1) };
}

export function KindChip({ kind }: { kind: string }) {
  return (
    <span className="rounded bg-black/5 px-1.5 py-0.5 text-xs font-normal dark:bg-white/10">
      {kind}
    </span>
  );
}
