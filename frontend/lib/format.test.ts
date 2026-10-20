import assert from "node:assert/strict";
import { test } from "node:test";
import type { T } from "./i18n/translate.ts";
import { compactTime, dayDistance, span, timeAgo } from "./format.ts";

// The times below are read in this zone, as a viewer there would see them.
process.env.TZ = "Europe/Istanbul";

/** A translator over the few messages these helpers use, in each language. */
function translator(locale: "en" | "tr"): T {
  const messages: Record<"en" | "tr", Record<string, string>> = {
    en: {
      "format.span.lessThanSecond": "<1s",
      "format.span.s": "{count}s",
      "format.span.m": "{count}m",
      "format.span.h": "{count}h",
      "format.span.d": "{count}d",
      "format.justNow": "just now",
      "format.seconds": "{count} s",
      "format.minutes": "{count} min",
      "format.ago": "{duration} ago",
    },
    tr: {
      "format.span.lessThanSecond": "<1 sn",
      "format.span.s": "{count} sn",
      "format.span.m": "{count} dk",
      "format.span.h": "{count} sa",
      "format.span.d": "{count} gün",
      "format.justNow": "şimdi",
      "format.seconds": "{count} sn",
      "format.minutes": "{count} dk",
      "format.ago": "{duration} önce",
    },
  };
  const t = ((key: string, params?: Record<string, string | number>) =>
    (messages[locale][key] ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(params?.[name] ?? ""))) as T;
  t.locale = locale;
  t.intl = locale === "tr" ? "tr-TR" : "en-US";
  t.n = (key, count, params) => t(`${key}.${count === 1 ? "one" : "other"}`, { count, ...params });
  return t;
}

const en = translator("en");
const tr = translator("tr");
const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

test("a span is its largest unit and the one below, the smaller one padded like a clock", () => {
  assert.equal(span(6 * SECOND, en), "6s");
  assert.equal(span(3 * MINUTE + 20 * SECOND, en), "3m 20s");
  assert.equal(span(20 * MINUTE + 5 * SECOND, en), "20m 05s");
  assert.equal(span(HOUR + 5 * MINUTE, en), "1h 05m");
  assert.equal(span(5 * DAY + 14 * HOUR, en), "5d 14h");
  assert.equal(span(2 * DAY + 4 * HOUR, en), "2d 4h");
  assert.equal(span(HOUR + 5 * MINUTE, tr), "1 sa 05 dk");
  assert.equal(span(5 * DAY + 14 * HOUR, tr), "5 gün 14 sa");
});

test("a span leaves out a smaller unit that is zero, and is never zero seconds", () => {
  assert.equal(span(2 * MINUTE, en), "2m");
  assert.equal(span(3 * HOUR, tr), "3 sa");
  assert.equal(span(DAY, en), "1d");
  assert.equal(span(DAY + 59 * SECOND, en), "1d");
  assert.equal(span(0, tr), "<1 sn");
  assert.equal(span(400, en), "<1s");
  assert.equal(span(-5000, en), "<1s");
  assert.doesNotMatch(span(2 * MINUTE + 300, tr), /0 sn/);
});

test("time ago says just now rather than zero seconds", () => {
  const now = Date.parse("2026-10-20T16:00:00Z");
  assert.equal(timeAgo("2026-10-20T16:00:00Z", now, tr), "şimdi");
  assert.equal(timeAgo("2026-10-20T15:59:48Z", now, en), "12 s ago");
  assert.equal(timeAgo("2026-10-20T15:57:00Z", now, tr), "3 dk önce");
});

test("days are counted on the calendar of the viewer's zone", () => {
  // 23:30 and 00:30 in Istanbul are an hour apart and a day apart.
  assert.equal(dayDistance(new Date("2026-10-20T21:30:00Z"), new Date("2026-10-20T20:30:00Z")), 1);
  assert.equal(dayDistance(new Date("2026-10-20T08:00:00Z"), new Date("2026-10-20T20:00:00Z")), 0);
});

test("a compact time grows only as far as it must", () => {
  const now = new Date("2026-10-20T16:00:00Z"); // Tuesday 19:00 in Istanbul
  assert.equal(compactTime(new Date("2026-10-20T16:11:00Z"), now, tr), "19:11");
  assert.equal(compactTime(new Date("2026-10-20T06:05:00Z"), now, en), "09:05");
  assert.equal(compactTime(new Date("2026-10-14T11:29:00Z"), now, tr), "Çar 14:29");
  assert.equal(compactTime(new Date("2026-10-14T11:29:00Z"), now, en), "Wed 14:29");
  // A window ahead is named the same way.
  assert.equal(compactTime(new Date("2026-10-22T19:00:00Z"), now, en), "Thu 22:00");
  assert.equal(compactTime(new Date("2026-10-02T11:29:00Z"), now, tr), "2 Eki 14:29");
  assert.equal(compactTime(new Date("2026-10-02T11:29:00Z"), now, en), "Oct 2 14:29");
  assert.equal(compactTime(new Date("2025-12-30T11:29:00Z"), now, tr), "30 Ara 2025 14:29");
});
