// The facts the fleet's summary states beyond its counts, worked out from
// the data the page already read: plain functions, so that they can be
// tested on their own.
import type { Monitor } from "@/lib/api";
import type { StripOutage } from "@/lib/fleet-strip";

/** A monitor among the slowest: its id, its latest answer in ms, and whether that is over its own threshold. */
export type SlowRow = { id: number; ms: number; slow: boolean };

/**
 * The monitors that answered slowest at their latest check, slowest first,
 * at most n of them: enabled, and up at that check, since a failed check
 * has no answer to measure. slow marks one over its own slow_above_ms.
 */
export function slowestMonitors(monitors: Pick<Monitor, "id" | "enabled" | "last_result" | "slow_above_ms">[], n: number): SlowRow[] {
  return monitors
    .filter((m) => m.enabled && m.last_result?.up)
    .map((m) => {
      const ms = m.last_result!.latency_ms;
      return { id: m.id, ms, slow: m.slow_above_ms > 0 && ms > m.slow_above_ms };
    })
    .sort((a, b) => b.ms - a.ms)
    .slice(0, Math.max(0, n));
}

/** The outage that ended last among the strip's; null while none has ended. */
export function lastEndedOutage(outages: StripOutage[]): (StripOutage & { end: number }) | null {
  let last: (StripOutage & { end: number }) | null = null;
  for (const o of outages) {
    if (o.end === null) continue;
    if (last === null || o.end > last.end) last = { ...o, end: o.end };
  }
  return last;
}
