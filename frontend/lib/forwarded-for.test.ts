import assert from "node:assert/strict";
import { test } from "node:test";
import { forwardedForHeader } from "./forwarded-for.ts";

test("a single address goes on as it came", () => {
  assert.deepEqual(forwardedForHeader("203.0.113.7"), { "X-Forwarded-For": "203.0.113.7" });
});

test("several addresses go on as they came, nothing added or reordered", () => {
  // What a client wrote to the left stays: the backend trusts only the
  // rightmost entries, which the proxies in front wrote.
  assert.deepEqual(forwardedForHeader("198.51.100.66, 203.0.113.7"), {
    "X-Forwarded-For": "198.51.100.66, 203.0.113.7",
  });
  assert.deepEqual(forwardedForHeader("198.51.100.66,203.0.113.7, 10.0.0.2"), {
    "X-Forwarded-For": "198.51.100.66,203.0.113.7, 10.0.0.2",
  });
});

test("without the header none is sent", () => {
  assert.deepEqual(forwardedForHeader(null), {});
  assert.deepEqual(forwardedForHeader(undefined), {});
  assert.deepEqual(forwardedForHeader(""), {});
  assert.deepEqual(forwardedForHeader("  "), {});
});
