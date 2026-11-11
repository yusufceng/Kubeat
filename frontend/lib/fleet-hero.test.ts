import assert from "node:assert/strict";
import { test } from "node:test";
import { lastEndedOutage, slowestMonitors } from "./fleet-hero.ts";

/** A latest check: up or not, and how long it took. */
function check(up: boolean, ms: number) {
  return { checked_at: "2026-11-11T10:00:00Z", up, latency_ms: ms, status_code: null, error: null, detail: null, cert_expires_at: null };
}

test("the slowest are the enabled monitors that answered, slowest first, cut at n", () => {
  const rows = slowestMonitors(
    [
      { id: 1, enabled: true, slow_above_ms: 0, last_result: check(true, 120) },
      { id: 2, enabled: true, slow_above_ms: 500, last_result: check(true, 900) },
      // Paused: its old answer is history, not the fleet as it stands.
      { id: 3, enabled: false, slow_above_ms: 0, last_result: check(true, 5000) },
      // A failed check has no answer to measure.
      { id: 4, enabled: true, slow_above_ms: 0, last_result: check(false, 7000) },
      // Never checked.
      { id: 5, enabled: true, slow_above_ms: 0, last_result: null },
      { id: 6, enabled: true, slow_above_ms: 1000, last_result: check(true, 400) },
    ],
    2,
  );
  assert.deepEqual(rows, [
    { id: 2, ms: 900, slow: true },
    { id: 6, ms: 400, slow: false },
  ]);
});

test("a threshold of zero is no threshold, and n of zero names nobody", () => {
  const monitors = [{ id: 1, enabled: true, slow_above_ms: 0, last_result: check(true, 99_000) }];
  assert.deepEqual(slowestMonitors(monitors, 3), [{ id: 1, ms: 99_000, slow: false }]);
  assert.deepEqual(slowestMonitors(monitors, 0), []);
});

test("the last outage is the one that ended last; an ongoing one does not count", () => {
  const outages = [
    { monitor: 1, name: "api", start: 1_000, end: 5_000 },
    { monitor: 2, name: "db", start: 2_000, end: null },
    { monitor: 3, name: "web", start: 3_000, end: 9_000 },
    { monitor: 4, name: "cache", start: 8_000, end: 8_500 },
  ];
  assert.deepEqual(lastEndedOutage(outages), { monitor: 3, name: "web", start: 3_000, end: 9_000 });
});

test("without an ended outage there is no last one", () => {
  assert.equal(lastEndedOutage([]), null);
  assert.equal(lastEndedOutage([{ monitor: 1, name: "api", start: 1_000, end: null }]), null);
});
