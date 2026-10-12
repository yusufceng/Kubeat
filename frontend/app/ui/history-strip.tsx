"use client";

import type { CheckResult } from "@/lib/api";
import { useMounted } from "@/lib/use-mounted";

/** One bar per check, oldest on the left; empty slots pad the left side. */
export function HistoryStrip({ results, slots }: { results: CheckResult[]; slots: number }) {
  // Hover titles carry the time in the viewer's zone, which only the browser
  // knows; until mounted they show the outcome alone.
  const mounted = useMounted();
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
          title={`${mounted ? `${new Date(r.checked_at).toLocaleTimeString("tr-TR")} · ` : ""}${
            r.up ? `${r.latency_ms} ms` : (r.error ?? "başarısız")
          }`}
          className={`w-1.5 rounded-sm ${r.up ? "bg-emerald-500" : "bg-red-500"}`}
        />
      ))}
    </div>
  );
}
