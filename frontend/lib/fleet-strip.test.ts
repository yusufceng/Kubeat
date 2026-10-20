import assert from "node:assert/strict";
import { test } from "node:test";
import { fleetSlots, occurrences } from "./fleet-strip.ts";

const H = 3_600_000;
const M = 60_000;
const now = Date.UTC(2026, 9, 20, 12, 0);

/** Four one-hour slots over the last four hours. */
function strip(opts: Partial<Parameters<typeof fleetSlots>[0]>) {
  return fleetSlots({ outages: [], windows: [], now, hours: 4, slots: 4, knownFrom: 0, ...opts });
}

test("a quiet fleet is healthy in every slot", () => {
  const slots = strip({});
  assert.equal(slots.length, 4);
  assert.deepEqual(
    slots.map((s) => s.state),
    ["ok", "ok", "ok", "ok"],
  );
  assert.equal(slots[0].start, now - 4 * H);
  assert.equal(slots[3].end, now);
});

test("an outage that lasts colors its slots down, a blip partial", () => {
  const slots = strip({
    outages: [
      { name: "pricing", start: now - 2 * H - 10 * M, end: null },
      { name: "search", start: now - 3 * H - 30 * M, end: now - 3 * H - 29 * M },
    ],
  });
  // pricing covers only the last 10 minutes of the second slot: it is
  // named there, but at least half of a slot is what makes it down.
  assert.deepEqual(
    slots.map((s) => s.state),
    ["partial", "partial", "down", "down"],
  );
  assert.deepEqual(slots[1].down, ["pricing"]);
  assert.deepEqual(slots[0].down, ["search"]);
});

test("outages of different monitors add up to the time covered, not their sum", () => {
  const slots = strip({
    outages: [
      { name: "a", start: now - H, end: now - 45 * M },
      { name: "b", start: now - 50 * M, end: now - 40 * M },
    ],
  });
  // 20 minutes of the last hour: partial, and both are named.
  assert.equal(slots[3].state, "partial");
  assert.deepEqual(slots[3].down, ["a", "b"]);
});

test("maintenance shows where nothing failed", () => {
  const slots = strip({
    windows: [{ name: "upgrade", start: now - 3 * H, end: now - 2 * H, repeat: "" }],
    outages: [{ name: "x", start: now - 30 * M, end: null }],
  });
  assert.deepEqual(
    slots.map((s) => s.state),
    ["ok", "maint", "ok", "down"],
  );
  assert.deepEqual(slots[1].maint, ["upgrade"]);
});

test("a daily window counts its earlier occurrences", () => {
  // Listed by its next occurrence, tonight; last night's is in the strip.
  const w = { name: "etl", start: now + 10 * H, end: now + 11 * H, repeat: "daily" as const };
  assert.deepEqual(occurrences(w, now - 24 * H, now), [[now - 14 * H, now - 13 * H]]);
  const slots = fleetSlots({ outages: [], windows: [w], now, hours: 24, slots: 24, knownFrom: 0 });
  assert.equal(slots.filter((s) => s.state === "maint").length, 1);
  assert.equal(slots[10].state, "maint");
});

test("a one-off window in the future is not in the strip", () => {
  const w = { name: "later", start: now + H, end: now + 2 * H, repeat: "" as const };
  assert.deepEqual(occurrences(w, now - 4 * H, now), []);
});

test("before what is known there is no data, never a healthy past", () => {
  const slots = strip({ knownFrom: now - 2 * H + 1 });
  assert.deepEqual(
    slots.map((s) => s.state),
    ["none", "none", "ok", "ok"],
  );
});
