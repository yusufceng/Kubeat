import assert from "node:assert/strict";
import { test } from "node:test";
import {
  kubectlName,
  maintenanceSpans,
  outagesWithin,
  percentParts,
  sameErrors,
  stepLength,
  upSince,
  uptimeTone,
  waterfall,
} from "./monitor-page.ts";

test("discovered objects are named the way kubectl names them", () => {
  assert.equal(kubectlName("service/pricing:8080"), "svc/pricing:8080");
  assert.equal(kubectlName("route/checkout-web"), "route/checkout-web");
  assert.equal(kubectlName("ingress/api-gateway"), "ing/api-gateway");
  assert.equal(kubectlName("deployment/payments-worker"), "deploy/payments-worker");
  assert.equal(kubectlName("statefulset/ledger-db"), "sts/ledger-db");
  assert.equal(kubectlName("daemonset/node-agent"), "ds/node-agent");
  assert.equal(kubectlName("cronjob/etl"), "cronjob/etl");
  assert.equal(kubectlName(""), null);
});

test("the good run starts at the end of the latest outage", () => {
  const monitor = { created_at: "2026-10-01T00:00:00Z" };
  assert.equal(upSince(monitor, []), "2026-10-01T00:00:00Z");
  assert.equal(
    upSince(monitor, [
      { kind: "down", resolved_at: "2026-10-10T10:00:00Z" },
      { kind: "down", resolved_at: "2026-10-14T11:54:24Z" },
      // A certificate warning does not interrupt the run.
      { kind: "cert", resolved_at: "2026-10-18T00:00:00Z" },
      { kind: "down", resolved_at: null },
    ]),
    "2026-10-14T11:54:24Z",
  );
});

test("outages within a period are clipped to it", () => {
  const h = 3_600_000;
  const at = (hours: number) => new Date(Date.UTC(2026, 9, 20) + hours * h).toISOString();
  const incidents = [
    { kind: "down" as const, started_at: at(-30), resolved_at: at(-29) }, // before the period
    { kind: "down" as const, started_at: at(-25), resolved_at: at(-23) }, // across its start: 1 h inside
    { kind: "cert" as const, started_at: at(-10), resolved_at: null }, // not an outage
    { kind: "down" as const, started_at: at(-2), resolved_at: null }, // still going on
  ];
  const to = Date.parse(at(0));
  assert.deepEqual(outagesWithin(incidents, to - 24 * h, to), { count: 2, downMs: 3 * h });
  assert.deepEqual(outagesWithin([], to - h, to), { count: 0, downMs: 0 });
});

test("uptime is amber below 99.9% and red below 99%", () => {
  assert.equal(uptimeTone(null), null);
  assert.equal(uptimeTone(100), null);
  assert.equal(uptimeTone(99.9), null);
  assert.equal(uptimeTone(99.89), "warn");
  assert.equal(uptimeTone(98.99), "down");
});

test("percents put the sign where the language does, and are floored", () => {
  assert.deepEqual(percentParts(99.55, "tr-TR"), { sign: "%", number: "99,55", signFirst: true });
  assert.deepEqual(percentParts(99.55, "en-US"), { sign: "%", number: "99.55", signFirst: false });
  assert.equal(percentParts(99.996, "en-US").number, "99.99");
  assert.equal(percentParts(100, "tr-TR").number, "100");
  assert.equal(percentParts(0, "en-US").number, "0.00");
});

test("the chart marks the maintenance that covers the monitor", () => {
  const windows = [
    { starts_at: "2026-10-16T16:31:04Z", ends_at: "2026-10-16T18:31:04Z", namespace: "", monitor_id: 20 },
    { starts_at: "2026-10-20T22:00:00Z", ends_at: "2026-10-20T23:00:00Z", namespace: "data", monitor_id: null },
    { starts_at: "2026-10-01T00:00:00Z", ends_at: "2026-10-01T01:00:00Z", namespace: "", monitor_id: null },
  ];
  assert.deepEqual(
    maintenanceSpans(windows, { id: 20, namespace: "observability" }).map((s) => new Date(s.from).toISOString()),
    ["2026-10-01T00:00:00.000Z", "2026-10-16T16:31:04.000Z"],
  );
  assert.equal(maintenanceSpans(windows, { id: 15, namespace: "data" }).length, 2);
  assert.equal(maintenanceSpans(windows, { id: 7, namespace: "checkout" }).length, 1);
});

test("a check's time falls into steps that follow one another", () => {
  const full = waterfall({ dns_ms: 2, connect_ms: 3, tls_ms: 10, first_byte_ms: 40, total_ms: 42 });
  assert.deepEqual(full.phases, [
    { key: "dns", state: "done", from: 0, to: 2 },
    { key: "connect", state: "done", from: 2, to: 5 },
    { key: "tls", state: "done", from: 5, to: 15 },
    { key: "firstByte", state: "done", from: 15, to: 40 },
  ]);
  assert.equal(full.total, 42);
  assert.equal(full.unanswered, null);

  // A reused connection to an IP address over plain HTTP: only the wait;
  // the steps before it were skipped.
  const reused = waterfall({ dns_ms: null, connect_ms: null, tls_ms: null, first_byte_ms: 21.3, total_ms: 21.5 });
  assert.deepEqual(reused.phases[3], { key: "firstByte", state: "done", from: 0, to: 21.3 });
  assert.deepEqual(reused.phases[0], { key: "dns", state: "skipped" });

  // Connected over plain HTTP, then no answer within the timeout: TLS and
  // the first byte were never reached.
  const silent = waterfall({ dns_ms: 1, connect_ms: 4, tls_ms: null, first_byte_ms: null, total_ms: 10_005 });
  assert.deepEqual(silent.unanswered, { from: 5, to: 10_005 });
  assert.deepEqual(silent.phases[2], { key: "tls", state: "unreached" });
  assert.deepEqual(silent.phases[3], { key: "firstByte", state: "unreached" });

  // A name that does not resolve: everything after the lookup is unreached.
  const lost = waterfall({ dns_ms: 4.2, connect_ms: null, tls_ms: null, first_byte_ms: null, total_ms: 4.3 });
  assert.deepEqual(
    lost.phases.map((p) => p.state),
    ["done", "unreached", "unreached", "unreached"],
  );
});

test("step lengths switch to seconds from a second on", () => {
  assert.deepEqual(stepLength(0.42, "en-US"), { value: "0.4", unit: "ms" });
  assert.deepEqual(stepLength(21.3, "tr-TR"), { value: "21", unit: "ms" });
  assert.deepEqual(stepLength(10_005, "tr-TR"), { value: "10", unit: "s" });
  assert.deepEqual(stepLength(1_550, "en-US"), { value: "1.6", unit: "s" });
});

test("errors that differ only in a length of time are listed once", () => {
  const push = (n: number, at: string) => ({
    error: `no push received for ${92 + n}h36m7s; one is expected every 1h0m0s`,
    count: 1,
    first_at: at,
    last_at: at,
  });
  const listed = sameErrors([
    push(0, "2026-10-20T10:00:00Z"),
    { error: "unexpected status 503", count: 2, first_at: "2026-10-20T09:00:00Z", last_at: "2026-10-20T09:30:00Z" },
    push(2, "2026-10-20T12:00:00Z"),
    push(1, "2026-10-20T11:00:00Z"),
    { error: "unexpected status 502", count: 1, first_at: "2026-10-20T08:00:00Z", last_at: "2026-10-20T08:00:00Z" },
  ]);
  assert.deepEqual(listed[0], {
    error: "no push received for 94h36m7s; one is expected every 1h0m0s",
    count: 3,
    first_at: "2026-10-20T10:00:00Z",
    last_at: "2026-10-20T12:00:00Z",
  });
  // A status code is not a length of time: 502 and 503 stay apart.
  assert.deepEqual(
    listed.map((e) => e.error).slice(1),
    ["unexpected status 503", "unexpected status 502"],
  );
});
