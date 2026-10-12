"use client";

import type { HistoryPoint } from "@/lib/api";
import { useMounted } from "@/lib/use-mounted";

/**
 * The recent checks of a monitor as one bar each, oldest on the left; empty
 * slots pad the left side so the newest check is always at the right edge.
 */
export function HeartbeatBar({ results, slots }: { results: HistoryPoint[]; slots: number }) {
  // Hover titles carry the time in the viewer's zone, which only the browser
  // knows; until mounted they show the outcome alone.
  const mounted = useMounted();
  const shown = results.slice(0, slots).reverse();
  const empty = Math.max(0, slots - shown.length);
  const failed = shown.filter((r) => !r.up).length;

  return (
    <div
      className="flex h-9 items-stretch gap-[3px]"
      role="img"
      aria-label={`Son ${shown.length} kontrolün ${failed} tanesi başarısız`}
    >
      {Array.from({ length: empty }, (_, i) => (
        <span key={`e${i}`} className="min-w-[2px] flex-1 rounded-full bg-sunken" />
      ))}
      {shown.map((r) => (
        <span
          key={r.t}
          title={`${mounted ? `${new Date(r.t).toLocaleString("tr-TR")} · ` : ""}${
            r.up ? `${r.ms} ms` : (r.error ?? "başarısız")
          }`}
          className={`min-w-[2px] flex-1 rounded-full transition-transform hover:scale-y-110 ${
            r.up ? "bg-up" : "bg-down"
          }`}
        />
      ))}
    </div>
  );
}
