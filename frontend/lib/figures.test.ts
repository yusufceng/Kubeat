import assert from "node:assert/strict";
import { test } from "node:test";
import { largestUnit, percentParts, uptimeTone } from "./figures.ts";

test("a period is told in its largest sensible unit", () => {
  assert.deepEqual(largestUnit(42), { value: 42, unit: "s" });
  assert.deepEqual(largestUnit(450), { value: 7.5, unit: "min" });
  assert.deepEqual(largestUnit(1800), { value: 30, unit: "min" });
  assert.deepEqual(largestUnit(12240), { value: 3.4, unit: "h" });
  assert.deepEqual(largestUnit(36 * 3600), { value: 36, unit: "h" });
  assert.deepEqual(largestUnit(10.3 * 86400), { value: 10, unit: "d" });
  assert.deepEqual(largestUnit(4.25 * 86400), { value: 4.3, unit: "d" });
  assert.deepEqual(largestUnit(-5), { value: 0, unit: "s" });
});

test("a percent keeps its sign apart, where the language puts it", () => {
  const text = (parts: { text: string; sign: boolean }[]) => parts.map((p) => (p.sign ? `[${p.text}]` : p.text)).join("");
  assert.equal(text(percentParts(99.73, "en-US")), "99.73[%]");
  assert.equal(text(percentParts(99.73, "tr-TR")), "[%]99,73");
  // Floored, so that an almost perfect uptime is not shown as a perfect one.
  assert.equal(text(percentParts(99.996, "en-US")), "99.99[%]");
  assert.equal(text(percentParts(100, "en-US")), "100.00[%]");
});

test("an uptime is coloured only once it is not healthy", () => {
  assert.equal(uptimeTone(null), "muted");
  assert.equal(uptimeTone(100), null);
  assert.equal(uptimeTone(99.9), null);
  assert.equal(uptimeTone(99.5), "warn");
  assert.equal(uptimeTone(98.2), "down");
});
