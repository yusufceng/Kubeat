"use client";

import type { HistoryPoint } from "@/lib/api";
import { beatState } from "@/lib/beats";
import { useT } from "@/lib/i18n/client";
import { BeatStrip } from "@/app/ui/beat-strip";

/**
 * The recent checks of a monitor in a table, one slot each, newest on the
 * right; a monitor with fewer checks than slots shows no data on the left.
 * A check slower than the monitor's threshold is amber and stands a step
 * taller, a failed one red and tallest. Hovering a slot tells when it ran
 * and what it came to. A paused monitor's checks are idle grey: history,
 * not how it stands.
 */
export function HeartbeatBar({
  results,
  slots,
  height = 30,
  paused = false,
  slowAboveMs = 0,
}: {
  /** Newest first, as the API lists them. */
  results: HistoryPoint[];
  slots: number;
  height?: number;
  paused?: boolean;
  /** A response slower than this many milliseconds is slow; 0 when the monitor has no threshold. */
  slowAboveMs?: number;
}) {
  const t = useT();
  const shown = results.slice(0, slots).toReversed();
  const states = shown.map((r) => beatState(r.up, r.ms, slowAboveMs));
  const failed = states.filter((state) => state === "down").length;
  const slow = states.filter((state) => state === "slow").length;
  return (
    <BeatStrip
      beats={shown.map((r, i) => ({ state: states[i], at: r.t, ms: r.ms, detail: r.up ? undefined : r.error }))}
      slots={slots}
      height={height}
      graded
      idle={paused}
      slowAboveMs={slowAboveMs}
      // One of a column of bars the pointer crosses on its way down the table.
      passing
      label={
        slow > 0
          ? t("beat.summarySlow", { count: shown.length, failed, slow })
          : t("beat.summary", { count: shown.length, failed })
      }
    />
  );
}
