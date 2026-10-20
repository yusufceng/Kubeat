/*
 * The fleet strip: the last hours of a fleet (or of one namespace) as
 * slots, oldest first, worked out from the outages and the maintenance
 * windows a page has already read, so it costs no request of its own.
 *
 * A slot is down when outages covered at least half of it, partial when an
 * outage touched it more briefly (a blip), maint when a planned window was
 * open and nothing failed, and ok otherwise. Before what is known, it is
 * none: before the oldest monitor existed, or before the oldest incident
 * read when the list was cut at its limit. A strip never claims a healthy
 * past it has no data for.
 *
 * How much of a slot is red (or amber) is how much of the fleet was down
 * in it: one monitor of many is a short bar, half the fleet or more the
 * whole of it. So one monitor down for days is a low band along the strip,
 * and what else failed meanwhile still stands out above it.
 *
 * No imports, so that the tests run it as it is.
 */

/** An outage of one monitor; end is null while it lasts. Times are Unix milliseconds. */
export type StripOutage = { monitor: number; name: string; start: number; end: number | null };

/**
 * A maintenance window. A repeating one is given by its current or next
 * occurrence, as the API lists it; the earlier ones are found by stepping
 * back. Times are Unix milliseconds.
 */
export type StripWindow = { name: string; start: number; end: number; repeat: "" | "daily" | "weekly" | "monthly" };

export type FleetSlot = {
  start: number;
  end: number;
  state: "ok" | "down" | "partial" | "maint" | "none";
  /** The monitors in an outage during the slot, by name. */
  down: string[];
  /**
   * The share of the slot's height its outages fill, 0 without any: the
   * square root of the share of the fleet down against half the fleet, so
   * that one, two or three monitors of many are told apart; at least
   * MIN_LEVEL, so that one monitor stays in sight.
   */
  level: number;
  /** The maintenance windows open during the slot, by name. */
  maint: string[];
};

const DAY = 86_400_000;

/** The least of a slot an outage fills, however large the fleet. */
export const MIN_LEVEL = 0.2;

/** How much of a slot n monitors down of a fleet of total fill. */
export function downLevel(n: number, total: number): number {
  if (n <= 0) return 0;
  return Math.min(1, Math.max(MIN_LEVEL, Math.sqrt((2 * n) / Math.max(total, n))));
}

/** The occurrences of a window that overlap [from, to), as [start, end) pairs. */
export function occurrences(w: StripWindow, from: number, to: number): [number, number][] {
  const out: [number, number][] = [];
  const length = w.end - w.start;
  let start = w.start;
  // Stepping back from the occurrence the API gives; a repeating window
  // keeps its time of day in its own zone, so across a change of daylight
  // saving time this is an hour off, which a strip of slots can bear.
  for (let k = 0; k < 400; k++) {
    const end = start + length;
    if (end <= from) break;
    if (start < to) out.push([Math.max(start, from), Math.min(end, to)]);
    if (w.repeat === "daily") start -= DAY;
    else if (w.repeat === "weekly") start -= 7 * DAY;
    else if (w.repeat === "monthly") {
      const d = new Date(start);
      d.setUTCMonth(d.getUTCMonth() - 1);
      start = d.getTime();
    } else break;
  }
  return out;
}

/** The length of the union of [start, end) pieces. */
function covered(pieces: [number, number][]): number {
  pieces.sort((a, b) => a[0] - b[0]);
  let total = 0;
  let reach = -Infinity;
  for (const [s, e] of pieces) {
    if (e <= reach) continue;
    total += e - Math.max(s, reach);
    reach = e;
  }
  return total;
}

export function fleetSlots({
  outages,
  windows,
  now,
  hours,
  slots,
  knownFrom,
  fleet,
}: {
  outages: StripOutage[];
  windows: StripWindow[];
  now: number;
  hours: number;
  slots: number;
  /** Before this, nothing is known (Unix milliseconds). */
  knownFrom: number;
  /** How many monitors the strip is about. */
  fleet: number;
}): FleetSlot[] {
  const from = now - hours * 3_600_000;
  const length = (now - from) / slots;
  const open = windows.flatMap((w) => occurrences(w, from, now).map(([s, e]) => ({ name: w.name, s, e })));
  const result: FleetSlot[] = [];
  for (let i = 0; i < slots; i++) {
    const start = from + i * length;
    const end = start + length;
    const pieces: [number, number][] = [];
    // The monitors down in the slot, each once, with its name.
    const down = new Map<number, string>();
    for (const o of outages) {
      const s = Math.max(o.start, start);
      const e = Math.min(o.end ?? now, end);
      // An outage that ended where it began still happened in the slot.
      if (e > s || (o.start >= start && o.start < end)) {
        down.set(o.monitor, o.name);
        if (e > s) pieces.push([s, e]);
      }
    }
    const maint = new Set(open.filter((w) => w.s < end && w.e > start).map((w) => w.name));
    let state: FleetSlot["state"];
    if (down.size > 0) state = covered(pieces) * 2 >= length ? "down" : "partial";
    else if (maint.size > 0) state = "maint";
    else if (end <= knownFrom) state = "none";
    else state = "ok";
    result.push({
      start,
      end,
      state,
      down: [...down.values()].sort(),
      level: downLevel(down.size, fleet),
      maint: [...maint].sort(),
    });
  }
  return result;
}
