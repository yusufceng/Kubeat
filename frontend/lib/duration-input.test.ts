import assert from "node:assert/strict";
import { test } from "node:test";
import { joinSeconds, splitSeconds } from "./duration-input.ts";

test("a stored length of time reads back in the largest unit that divides it", () => {
  assert.deepEqual(splitSeconds(600), { count: 10, unit: "m" });
  assert.deepEqual(splitSeconds(3600), { count: 1, unit: "h" });
  assert.deepEqual(splitSeconds(5400), { count: 90, unit: "m" });
  assert.deepEqual(splitSeconds(172800), { count: 2, unit: "d" });
  assert.deepEqual(splitSeconds(90), { count: 90, unit: "s" });
});

test("zero and nonsense are 0 minutes, the unit most values take", () => {
  assert.deepEqual(splitSeconds(0), { count: 0, unit: "m" });
  assert.deepEqual(splitSeconds(-5), { count: 0, unit: "m" });
});

test("a typed count and unit become whole seconds, or null when the count is not a whole number", () => {
  assert.equal(joinSeconds("10", "m"), 600);
  assert.equal(joinSeconds("1", "h"), 3600);
  assert.equal(joinSeconds("0", "d"), 0);
  assert.equal(joinSeconds("", "m"), null);
  assert.equal(joinSeconds("1.5", "h"), null);
  assert.equal(joinSeconds("-1", "s"), null);
});
