import assert from "node:assert/strict";
import { test } from "node:test";
import { byDay, dayKey, inProgress, newlyDown, utf8Length, worsened } from "./status-page.ts";

test("worsened: only a climb from calm towards down counts", () => {
  assert.equal(worsened("up", "down"), true);
  assert.equal(worsened("up", "degraded"), true);
  assert.equal(worsened("maintenance", "degraded"), true);
  assert.equal(worsened("degraded", "down"), true);
  assert.equal(worsened("down", "degraded"), false);
  assert.equal(worsened("down", "up"), false);
  assert.equal(worsened("up", "up"), false);
  assert.equal(worsened("up", "maintenance"), true);
});

test("newlyDown: the ids down now that were not down before", () => {
  assert.deepEqual(newlyDown([7], [7, 9]), [9]);
  assert.deepEqual(newlyDown([7, 9], [7]), []);
  assert.deepEqual(newlyDown([], [1, 2]), [1, 2]);
  assert.deepEqual(newlyDown(new Set([1]), [1]), []);
});

test("dayKey: UTC until the browser takes over", () => {
  assert.equal(dayKey("2026-10-14T23:30:00Z", false), "2026-10-14");
  const d = new Date("2026-10-14T23:30:00Z");
  const local = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  assert.equal(dayKey("2026-10-14T23:30:00Z", true), local);
});

test("byDay: newest day first, newest update first within it, whatever order they came in", () => {
  const updates = [
    { id: 1, created_at: "2026-10-13T09:00:00Z" },
    { id: 3, created_at: "2026-10-14T19:11:00Z" },
    { id: 2, created_at: "2026-10-14T08:02:00Z" },
  ];
  const days = byDay(updates, false);
  assert.deepEqual(
    days.map((d) => ({ day: d.day, ids: d.updates.map((u) => u.id) })),
    [
      { day: "2026-10-14", ids: [3, 2] },
      { day: "2026-10-13", ids: [1] },
    ],
  );
  assert.deepEqual(byDay([], false), []);
});

test("utf8Length: counts bytes the way the backend measures a title", () => {
  assert.equal(utf8Length("abc"), 3);
  assert.equal(utf8Length("Ödeme"), 6);
  assert.equal(utf8Length("ağ"), 3);
  assert.equal(utf8Length("🙂"), 4);
  assert.equal(utf8Length(""), 0);
});

test("inProgress: a window that has begun and not ended", () => {
  const window = { starts_at: "2026-10-14T10:00:00Z", ends_at: "2026-10-14T12:00:00Z" };
  assert.equal(inProgress(window, Date.parse("2026-10-14T11:00:00Z")), true);
  assert.equal(inProgress(window, Date.parse("2026-10-14T10:00:00Z")), true);
  assert.equal(inProgress(window, Date.parse("2026-10-14T12:00:00Z")), false);
  assert.equal(inProgress(window, Date.parse("2026-10-14T09:59:59Z")), false);
});
