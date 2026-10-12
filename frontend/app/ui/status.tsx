import type { CheckResult, Monitor } from "@/lib/api";

/**
 * What the UI shows for a monitor. It combines the latest check with the
 * incident state: one failed check is not yet an outage, and one successful
 * check does not always end one.
 */
export type DisplayState = "up" | "down" | "verifying" | "recovering" | "pending" | "paused";

export function displayState(m: Pick<Monitor, "status" | "open_incident">): DisplayState {
  if (m.status === "paused") return "paused";
  if (m.open_incident) return m.status === "up" ? "recovering" : "down";
  return m.status === "down" ? "verifying" : m.status;
}

const styles: Record<DisplayState, { dot: string; text: string; label: string }> = {
  up: { dot: "bg-emerald-500", text: "text-emerald-700 dark:text-emerald-400", label: "Çalışıyor" },
  down: { dot: "bg-red-500", text: "text-red-700 dark:text-red-400", label: "Erişilemiyor" },
  verifying: {
    dot: "bg-amber-500",
    text: "text-amber-700 dark:text-amber-400",
    label: "Doğrulanıyor",
  },
  recovering: {
    dot: "bg-amber-500",
    text: "text-amber-700 dark:text-amber-400",
    label: "Toparlanıyor",
  },
  pending: { dot: "bg-zinc-400", text: "text-zinc-500", label: "Bekleniyor" },
  paused: { dot: "bg-zinc-400", text: "text-zinc-500", label: "Durduruldu" },
};

export function StatusBadge({ state }: { state: DisplayState }) {
  const s = styles[state];
  return (
    <span className={`inline-flex items-center gap-2 text-sm font-medium ${s.text}`}>
      <span className={`h-2.5 w-2.5 rounded-full ${s.dot}`} aria-hidden />
      {s.label}
    </span>
  );
}

/** One bar per check, oldest on the left; empty slots pad the left side. */
export function HistoryStrip({ results, slots }: { results: CheckResult[]; slots: number }) {
  const shown = results.slice(0, slots).reverse();
  const empty = Math.max(0, slots - shown.length);
  const failed = shown.filter((r) => !r.up).length;

  return (
    <div
      className="flex h-6 items-stretch gap-px"
      role="img"
      aria-label={`Son ${shown.length} kontrolün ${failed} tanesi başarısız`}
    >
      {Array.from({ length: empty }, (_, i) => (
        <span key={`e${i}`} className="w-1.5 rounded-sm bg-zinc-200 dark:bg-zinc-800" />
      ))}
      {shown.map((r) => (
        <span
          key={r.checked_at}
          title={`${new Date(r.checked_at).toLocaleTimeString("tr-TR")} · ${
            r.up ? `${r.latency_ms} ms` : (r.error ?? "başarısız")
          }`}
          className={`w-1.5 rounded-sm ${r.up ? "bg-emerald-500" : "bg-red-500"}`}
        />
      ))}
    </div>
  );
}
