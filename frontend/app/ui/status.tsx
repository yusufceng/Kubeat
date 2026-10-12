import type { Monitor } from "@/lib/api";

/**
 * What the UI shows for a monitor. It combines the latest check with the
 * incident state: one failed check is not yet an outage, and one successful
 * check does not always end one.
 */
export type DisplayState =
  | "up"
  | "down"
  | "verifying"
  | "recovering"
  | "maintenance"
  | "pending"
  | "paused";

export function displayState(
  m: Pick<Monitor, "status" | "open_incident" | "maintenance_until">,
): DisplayState {
  if (m.status === "paused") return "paused";
  if (m.open_incident) return m.status === "up" ? "recovering" : "down";
  // In maintenance nothing is alerted on, whatever the checks say.
  if (m.maintenance_until && m.status !== "pending") return "maintenance";
  // A failing monitor without an incident is still being verified.
  return m.status === "down" ? "verifying" : m.status;
}

export const stateStyles: Record<DisplayState, { dot: string; text: string; soft: string; label: string }> = {
  up: { dot: "bg-up", text: "text-up", soft: "bg-up/12 text-up", label: "Çalışıyor" },
  down: { dot: "bg-down", text: "text-down", soft: "bg-down/12 text-down", label: "Erişilemiyor" },
  verifying: { dot: "bg-warn", text: "text-warn", soft: "bg-warn/15 text-warn", label: "Doğrulanıyor" },
  recovering: { dot: "bg-warn", text: "text-warn", soft: "bg-warn/15 text-warn", label: "Toparlanıyor" },
  maintenance: { dot: "bg-maint", text: "text-maint", soft: "bg-maint/12 text-maint", label: "Bakımda" },
  pending: { dot: "bg-idle", text: "text-muted", soft: "bg-sunken text-muted", label: "Bekleniyor" },
  paused: { dot: "bg-idle", text: "text-muted", soft: "bg-sunken text-muted", label: "Durduruldu" },
};

export function StatusBadge({ state }: { state: DisplayState }) {
  const s = stateStyles[state];
  return (
    <span className={`inline-flex items-center gap-2 text-sm font-medium ${s.text}`}>
      <span className={`h-2.5 w-2.5 rounded-full ${s.dot}`} aria-hidden />
      {s.label}
    </span>
  );
}

/** The state as a filled pill, for headers. */
export function StatusPill({ state }: { state: DisplayState }) {
  const s = stateStyles[state];
  return (
    <span className={`inline-flex items-center gap-2 rounded-full px-3 py-1 text-sm font-semibold ${s.soft}`}>
      <span className={`h-2 w-2 rounded-full ${s.dot}`} aria-hidden />
      {s.label}
    </span>
  );
}
