import assert from "node:assert/strict";
import { test } from "node:test";
import { gatherRuns, isBlip, MIN_RUN, runExtent, type Spell } from "./flaps.ts";

const WINDOW = 10 * 60_000;
const at = (minutes: number, seconds = 0) => new Date(Date.UTC(2026, 9, 20, 21, minutes, seconds)).toISOString();

type Outage = Spell & { id: number };

/** An outage of a monitor from minute m, lasting s seconds; open when s is null. */
function outage(id: number, monitor: number, m: number, s: number | null): Outage {
  return {
    id,
    monitor_id: monitor,
    kind: "down",
    started_at: at(m),
    resolved_at: s === null ? null : new Date(Date.parse(at(m)) + s * 1000).toISOString(),
  };
}

test("a short outage that is over is a blip; a long or an open one is not", () => {
  assert.equal(isBlip(outage(1, 27, 0, 20), WINDOW), true);
  assert.equal(isBlip(outage(2, 7, 0, 3600), WINDOW), false);
  assert.equal(isBlip(outage(3, 7, 0, null), WINDOW), false);
  assert.equal(isBlip({ ...outage(4, 7, 0, 20), kind: "cert" }, WINDOW), false);
});

test("short outages of one monitor that follow each other are gathered, from the third on", () => {
  // Newest first, as the incident list has them.
  const list = [
    outage(9, 27, 29, 10),
    outage(8, 27, 27, 30),
    outage(7, 27, 24, 10),
    outage(6, 7, 20, null),
    outage(5, 27, 14, 20),
    outage(4, 27, 12, 20),
    outage(3, 27, 1, 20),
  ];
  const rows = gatherRuns(list, WINDOW);
  assert.equal(rows.length, 5);
  assert.deepEqual(
    (rows[0] as Outage[]).map((i) => i.id),
    [9, 8, 7],
  );
  // The open outage of another monitor stays a row of its own, and the two
  // after it are too few to gather; the last one is too far from them.
  assert.deepEqual(
    rows.slice(1).map((r) => (Array.isArray(r) ? "run" : r.id)),
    [6, 5, 4, 3],
  );
  assert.equal(MIN_RUN, 3);
});

test("a run's extent is from its first start to its last end, with its shortest and longest outage", () => {
  const run = [outage(9, 27, 29, 10), outage(8, 27, 27, 30), outage(7, 27, 24, 10)];
  assert.deepEqual(runExtent(run), {
    count: 3,
    from: at(24),
    to: new Date(Date.parse(at(29)) + 10_000).toISOString(),
    shortest: 10_000,
    longest: 30_000,
  });
});

test("without a window every outage is its own", () => {
  const list = [outage(3, 27, 3, 10), outage(2, 27, 2, 10), outage(1, 27, 1, 10)];
  assert.deepEqual(gatherRuns(list, 0), list);
});
