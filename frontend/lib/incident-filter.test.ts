import assert from "node:assert/strict";
import { test } from "node:test";
import {
  csvHref,
  emptyFilter,
  filterHref,
  filterParams,
  filterToQuery,
  isFiltered,
  parseIncidentFilter,
  rangeKey,
} from "./incident-filter.ts";

const ranges = [86400, 604800, 2592000];

test("an address without a query is the whole list, newest first", () => {
  assert.deepEqual(parseIncidentFilter({}, ranges), emptyFilter);
  assert.equal(isFiltered(emptyFilter), false);
  assert.equal(filterHref(emptyFilter), "/incidents");
});

test("what the address says is read, and what it cannot use is left out", () => {
  const f = parseIncidentFilter(
    new URLSearchParams(
      "namespace=shop&namespace=&namespace=shop&kind=cert&state=open&monitor=12&q=%20api%20&sort=duration&order=asc&page=3",
    ),
    ranges,
  );
  assert.deepEqual(f.namespaces, ["shop", ""]);
  assert.equal(f.kind, "cert");
  assert.equal(f.state, "open");
  assert.equal(f.monitorId, 12);
  assert.equal(f.q, "api");
  assert.equal(f.sort, "duration");
  assert.equal(f.order, "asc");
  assert.equal(f.page, 3);

  const bad = parseIncidentFilter({ kind: "x", state: "gone", monitor: "-1", sort: "id", order: "up", page: "0" }, ranges);
  assert.deepEqual(bad, emptyFilter);
});

test("only the offered periods are taken, and an own period must end after it starts", () => {
  assert.equal(parseIncidentFilter({ range: "7d" }, ranges).range, 604800);
  assert.equal(parseIncidentFilter({ range: "5d" }, ranges).range, null);
  const own = parseIncidentFilter({ from: "2026-10-01T00:00:00+03:00", to: "2026-10-02T00:00:00Z" }, ranges);
  assert.equal(own.from, "2026-09-30T21:00:00.000Z");
  assert.equal(own.to, "2026-10-02T00:00:00.000Z");
  const backwards = parseIncidentFilter({ from: "2026-10-02T00:00:00Z", to: "2026-10-01T00:00:00Z" }, ranges);
  assert.equal(backwards.from, null);
  assert.equal(backwards.to, null);
  assert.equal(parseIncidentFilter({ from: "2026" }, ranges).from, null);
  // An offered period wins over an own one.
  assert.equal(parseIncidentFilter({ range: "24h", from: "2026-10-01T00:00:00Z" }, ranges).from, null);
});

test("the text is cut to what the backend accepts", () => {
  assert.equal(parseIncidentFilter({ q: "a".repeat(150) }, ranges).q.length, 100);
});

test("periods are written the short way", () => {
  assert.equal(rangeKey(86400), "24h");
  assert.equal(rangeKey(172800), "2d");
  assert.equal(rangeKey(7200), "2h");
  assert.equal(rangeKey(5400), "90m");
  assert.equal(rangeKey(45), "45s");
});

test("the backend is asked with its own names, and the default order is left out", () => {
  const now = Date.parse("2026-10-20T12:00:00Z");
  const f = parseIncidentFilter({ namespace: ["", "shop"], monitor: "7", range: "24h", page: "2" }, ranges);
  assert.deepEqual(filterToQuery(f, now, 25), {
    namespaces: ["", "shop"],
    kind: undefined,
    state: undefined,
    monitorId: 7,
    q: undefined,
    from: "2026-10-19T12:00:00.000Z",
    to: undefined,
    sort: undefined,
    order: undefined,
    limit: 25,
    offset: 25,
  });
  const reversed = parseIncidentFilter({ sort: "monitor", order: "desc" }, ranges);
  const q = filterToQuery(reversed, now);
  assert.equal(q.sort, "monitor");
  assert.equal(q.order, "desc");
  assert.equal("limit" in q, false);
});

test("links keep the filter, and a new filter or sort starts at the first page", () => {
  const f = parseIncidentFilter({ state: "open", namespace: "shop", page: "4" }, ranges);
  assert.equal(filterHref(f, { page: 5 }), "/incidents?state=open&namespace=shop&page=5");
  assert.equal(filterHref(f, { kind: "down" }), "/incidents?state=open&kind=down&namespace=shop");
  assert.equal(filterHref(f, { sort: "monitor" }), "/incidents?state=open&namespace=shop&sort=monitor");
  assert.equal(filterHref(f, { sort: "monitor", order: "desc" }), "/incidents?state=open&namespace=shop&sort=monitor&order=desc");
  assert.equal(filterHref(f, { state: null }), "/incidents?namespace=shop");
  assert.equal(filterParams(f, { namespaces: [""] }).toString(), "state=open&namespace=");
  assert.equal(csvHref(f), "/incidents/csv?state=open&namespace=shop");
  assert.equal(csvHref(emptyFilter), "/incidents/csv");
});
