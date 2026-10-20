"use client";

import type { HistoryPoint } from "@/lib/api";
import { milliseconds } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import { BeatStrip } from "@/app/ui/beat-strip";

/**
 * The recent checks of a monitor, one slot each, newest on the right; a
 * monitor with fewer checks than slots shows no data on the left. Hovering
 * a slot tells when it ran and what it came to.
 */
export function HeartbeatBar({
  results,
  slots,
  height = 30,
}: {
  /** Newest first, as the API lists them. */
  results: HistoryPoint[];
  slots: number;
  height?: number;
}) {
  const t = useT();
  const shown = results.slice(0, slots).toReversed();
  const failed = shown.filter((r) => !r.up).length;
  return (
    <BeatStrip
      beats={shown.map((r) => ({
        state: r.up ? "ok" : "down",
        at: r.t,
        text: r.up ? milliseconds(r.ms, t) : t("monitorPage.heartbeat.failed"),
        detail: r.up ? undefined : r.error,
      }))}
      slots={slots}
      height={height}
      label={t("monitorPage.heartbeat.summary", { shown: shown.length, failed })}
    />
  );
}
