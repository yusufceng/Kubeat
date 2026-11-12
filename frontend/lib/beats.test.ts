import assert from "node:assert/strict";
import { test } from "node:test";
import {
  beatState,
  decodeBeatLatencies,
  decodeBeats,
  decodeBeatTimes,
  encodeBeatLatencies,
  encodeBeats,
  encodeBeatTimes,
} from "./beats.ts";

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

test("a symbol it does not know is a check that passed, not an outage", () => {
  assert.deepEqual(decodeBeats("x"), ["ok"]);
});

test("one check is judged by the rule the string is written with", () => {
  assert.equal(beatState(true, 1000, 1000), "ok");
  assert.equal(beatState(true, 1000.4, 1000), "slow");
  assert.equal(beatState(true, 60_000, 0), "ok");
  assert.equal(beatState(false, 60_000, 1000), "down");
});

test("times are written oldest first, as the first second and the steps after it", () => {
  const history = [{ t: 1_794_000_090_999 }, { t: 1_794_000_030_500 }, { t: 1_794_000_000_000 }];
  const times = encodeBeatTimes(history);
  assert.equal(times, `${(1_794_000_000).toString(36)}.u.1o`);
  // To the second: what a hover shows.
  assert.deepEqual(decodeBeatTimes(times), [1_794_000_000_000, 1_794_000_030_000, 1_794_000_090_000]);
  assert.equal(encodeBeatTimes([]), "");
  assert.deepEqual(decodeBeatTimes(""), []);
});

test("a time that runs backwards still comes back as it was", () => {
  const history = [{ t: 1_794_000_000_000 }, { t: 1_794_000_030_000 }];
  assert.deepEqual(decodeBeatTimes(encodeBeatTimes(history)), [1_794_000_030_000, 1_794_000_000_000]);
});

test("latencies are written oldest first, in whole milliseconds", () => {
  const history = [{ ms: 8014 }, { ms: 0 }, { ms: 37.6 }];
  const latencies = encodeBeatLatencies(history);
  assert.equal(latencies, "12.0.66m");
  assert.deepEqual(decodeBeatLatencies(latencies), [38, 0, 8014]);
  assert.equal(encodeBeatLatencies([]), "");
  assert.deepEqual(decodeBeatLatencies(""), []);
});

test("the three strings of a row tell of the same checks, one for one", () => {
  const history = [
    { t: 1_794_000_060_000, up: true, ms: 1200 },
    { t: 1_794_000_030_000, up: false, ms: 10_000 },
    { t: 1_794_000_000_000, up: true, ms: 41 },
  ];
  const states = decodeBeats(encodeBeats(history, 1000));
  const times = decodeBeatTimes(encodeBeatTimes(history));
  const latencies = decodeBeatLatencies(encodeBeatLatencies(history));
  assert.deepEqual(
    states.map((state, i) => [state, times[i], latencies[i]]),
    [
      ["ok", 1_794_000_000_000, 41],
      ["down", 1_794_000_030_000, 10_000],
      ["slow", 1_794_000_060_000, 1200],
    ],
  );
});
