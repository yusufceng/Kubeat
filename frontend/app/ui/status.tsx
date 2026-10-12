import type { CheckResult, Monitor } from "@/lib/api";

const styles = {
  up: { dot: "bg-emerald-500", text: "text-emerald-700 dark:text-emerald-400", label: "Çalışıyor" },
  down: { dot: "bg-red-500", text: "text-red-700 dark:text-red-400", label: "Erişilemiyor" },
  pending: { dot: "bg-zinc-400", text: "text-zinc-500", label: "Bekleniyor" },
} as const;

export function StatusBadge({ status }: { status: Monitor["status"] }) {
  const s = styles[status];
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
