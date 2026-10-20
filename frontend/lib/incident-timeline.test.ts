import assert from "node:assert/strict";
import { test } from "node:test";
import type { CheckResult, TimelineItem } from "./api.ts";
import { byDay, checkError, checkSignature, deliveries, kubectlName, timelineRows, typeWord } from "./incident-timeline.ts";

const utcDay = (iso: string) => iso.slice(0, 10);

function failed(at: string, error: string, code: string | null = null, status: number | null = null): TimelineItem {
  const check: CheckResult = {
    checked_at: at,
    up: false,
    latency_ms: 12,
    status_code: status,
    error,
    detail: null,
    cert_expires_at: null,
    diagnostics: code
      ? {
          explanation: { code, params: {}, text: "" },
          timing: null,
          remote_addr: null,
          tls: null,
          status_line: null,
          headers: {},
          redirects: 0,
          pod: null,
        }
      : null,
  };
  return { at, kind: "check_failed", check };
}

const notification = (at: string, channel: number, delivered: boolean): TimelineItem => ({
  at,
  kind: "notification",
  notification: {
    event: "down",
    event_at: at,
    channel_id: channel,
    channel_name: `c${channel}`,
    channel_type: "webhook",
    channel_deleted: false,
    grouped: false,
    group_size: 1,
    delivered,
  },
});

test("an error says its status code when it does not already", () => {
  assert.equal(checkError({ status_code: 503, error: "unexpected status 503", detail: null }), "unexpected status 503");
  assert.equal(checkError({ status_code: 502, error: "bad gateway", detail: null }), "HTTP 502 · bad gateway");
  assert.equal(checkError({ status_code: 404, error: null, detail: null }), "HTTP 404");
  assert.equal(checkError({ status_code: null, error: null, detail: "keyword missing" }), "keyword missing");
});

test("checks that failed the same way share a signature, whatever their figures", () => {
  const sig = (item: TimelineItem) => (item.kind === "check_failed" ? checkSignature(item.check) : "");
  assert.equal(
    sig(failed("2026-10-20T10:00:00Z", "no push received for 92h36m7s")),
    sig(failed("2026-10-20T10:01:00Z", "no push received for 92h37m7s")),
  );
  assert.notEqual(
    sig(failed("2026-10-20T10:00:00Z", "unexpected status 503", "http_503", 503)),
    sig(failed("2026-10-20T10:00:00Z", "connection refused", "connection_refused")),
  );
});

test("the first failed check stands alone and the same failures after it are gathered", () => {
  const items: TimelineItem[] = [
    failed("2026-10-20T10:00:00Z", "unexpected status 503", "http_503", 503),
    failed("2026-10-20T10:00:15Z", "unexpected status 503", "http_503", 503),
    { at: "2026-10-20T10:00:15Z", kind: "opened", opened: { failure_threshold: 2, cause: "unexpected status 503" } },
    notification("2026-10-20T10:00:16Z", 1, true),
    failed("2026-10-20T10:00:30Z", "unexpected status 503", "http_503", 503),
    failed("2026-10-20T10:00:45Z", "unexpected status 503", "http_503", 503),
    failed("2026-10-20T10:01:00Z", "dial tcp 10.0.0.1:80: connect: connection refused", "connection_refused"),
    { at: "2026-10-20T10:01:15Z", kind: "checks_omitted", omitted: { count: 40, from: "2026-10-20T10:01:15Z", to: "2026-10-20T10:11:15Z" } },
    failed("2026-10-20T10:11:30Z", "dial tcp 10.0.0.1:80: connect: connection refused", "connection_refused"),
    { at: "2026-10-20T10:11:45Z", kind: "resolved", resolved: { duration_seconds: 705 } },
  ];
  const rows = timelineRows(items, "2026-10-20T10:00:00Z", utcDay);
  assert.deepEqual(
    rows.map((r) => (r.type === "item" ? r.item.kind : r.type === "checks" ? `checks×${r.checks.length}` : r.type)),
    ["first", "checks×1", "opened", "notification", "checks×2", "checks×1", "checks_omitted", "checks×1", "resolved"],
  );
  assert.equal(new Set(rows.map((r) => r.key)).size, rows.length);
});

test("a run of the same failure ends where the day does", () => {
  const items = [
    failed("2026-10-20T23:59:30Z", "timeout", "timeout"),
    failed("2026-10-20T23:59:45Z", "timeout", "timeout"),
    failed("2026-10-21T00:00:00Z", "timeout", "timeout"),
  ];
  const rows = timelineRows(items, "2026-10-20T23:00:00Z", utcDay);
  assert.deepEqual(
    rows.map((r) => (r.type === "checks" ? r.checks.length : 0)),
    [2, 1],
  );
  assert.deepEqual(
    byDay(rows, utcDay).map((d) => [d.day, d.rows.length]),
    [
      ["2026-10-20", 1],
      ["2026-10-21", 1],
    ],
  );
});

test("the notifications are counted, those left out too", () => {
  const items: TimelineItem[] = [
    notification("2026-10-20T10:00:16Z", 1, true),
    notification("2026-10-20T10:00:26Z", 2, false),
    { at: "2026-10-20T10:05:00Z", kind: "notifications_omitted", omitted: { count: 7, from: "2026-10-20T10:05:00Z", to: "2026-10-20T11:00:00Z" } },
    notification("2026-10-20T11:00:16Z", 1, true),
  ];
  assert.deepEqual(deliveries(items), { attempts: 10, failed: 1, omitted: 7, channels: 2 });
  assert.deepEqual(deliveries([]), { attempts: 0, failed: 0, omitted: 0, channels: 0 });
});

test("discovered objects are named the way kubectl names them", () => {
  assert.equal(kubectlName("service/pricing:8080"), "svc/pricing:8080");
  assert.equal(kubectlName("route/checkout-web"), "route/checkout-web");
  assert.equal(kubectlName("deployment/payments-worker"), "deploy/payments-worker");
  assert.equal(kubectlName(""), null);
  assert.equal(typeWord("websocket"), "ws");
  assert.equal(typeWord("http"), "http");
});
