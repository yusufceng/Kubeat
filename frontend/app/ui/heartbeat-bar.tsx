"use client";

import type { HistoryPoint } from "@/lib/api";
import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";

/**
 * The recent checks of a monitor as one bar each, oldest on the left; empty
 * slots pad the left side so the newest check is always at the right edge.
 */
export function HeartbeatBar({ results, slots }: { results: HistoryPoint[]; slots: number }) {
  const t = useT();
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
      aria-label={t("monitorPage.heartbeat.summary", { shown: shown.length, failed })}
    >
      {Array.from({ length: empty }, (_, i) => (
        <span key={`e${i}`} className="min-w-[2px] flex-1 rounded-full bg-sunken" />
      ))}
      {shown.map((r) => (
        <span
          key={r.t}
          title={`${mounted ? `${new Date(r.t).toLocaleString(t.intl)} · ` : ""}${
            r.up ? `${r.ms} ms` : (r.error ?? t("monitorPage.heartbeat.failed"))
          }`}
          className={`min-w-[2px] flex-1 rounded-full transition-transform hover:scale-y-110 ${
            r.up ? "bg-up" : "bg-down"
          }`}
        />
      ))}
    </div>
  );
}
