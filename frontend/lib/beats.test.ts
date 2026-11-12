import assert from "node:assert/strict";
import { test } from "node:test";
import { decodeBeats, encodeBeats } from "./beats.ts";

test("beats are written oldest first, from a history that is newest first", () => {
  const history = [
    { up: true, ms: 40 },
    { up: false, ms: 0 },
    { up: true, ms: 35 },
  ];
  assert.equal(encodeBeats(history, 0), "101");
  assert.deepEqual(decodeBeats("101"), ["ok", "down", "ok"]);
  assert.equal(encodeBeats([], 0), "");
  assert.deepEqual(decodeBeats(""), []);
});

test("a success slower than the threshold is its own beat, and one at the threshold is not", () => {
  const history = [
    { up: true, ms: 1001 },
    { up: true, ms: 1000 },
    { up: true, ms: 12 },
  ];
  assert.equal(encodeBeats(history, 1000), "112");
  assert.deepEqual(decodeBeats("112"), ["ok", "ok", "slow"]);
});

test("without a threshold nothing is slow, and a failure is never slow", () => {
  assert.equal(encodeBeats([{ up: true, ms: 60_000 }], 0), "1");
  assert.equal(encodeBeats([{ up: false, ms: 60_000 }], 1000), "0");
});

test("a round trip keeps every check", () => {
  const history = [
    { up: false, ms: 5000 },
    { up: true, ms: 900 },
    { up: true, ms: 80 },
    { up: false, ms: 3 },
  ];
  const states = decodeBeats(encodeBeats(history, 500));
  assert.deepEqual(states, ["down", "ok", "slow", "down"]);
});
