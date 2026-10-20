"use client";

import { clock, dayDistance } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { useMounted } from "@/lib/use-mounted";

/** A day as a visitor would say it: "Bugün", "Dün", "Çar 14 Eki", "14 Eki 2025". */
function dayName(date: Date, now: Date, t: T): string {
  const days = dayDistance(now, date);
  if (days === 0) return t("time.today");
  if (days === 1) return t("time.yesterday");
  if (date.getFullYear() === now.getFullYear()) {
    return date.toLocaleDateString(t.intl, { weekday: "short", day: "numeric", month: "short" });
  }
  return date.toLocaleDateString(t.intl, { day: "numeric", month: "short", year: "numeric" });
}

/**
 * When an outage was, in the visitor's zone on a 24-hour clock: "Bugün
 * 13:29–13:31", "Çar 14 Eki 14:29–14:54", across midnight "Dün 23:50 –
 * Bugün 00:20", and for one still going on "Bugün 19:11 itibarıyla". The
 * server cannot know the zone, so the first render is in UTC, labelled.
 */
export function IncidentWhen({ start, end }: { start: string; end: string | null }) {
  const t = useT();
  const mounted = useMounted();
  if (!mounted) {
    const utc = (iso: string) => `${iso.slice(0, 10)} ${iso.slice(11, 16)}`;
    return <time dateTime={start}>{`${utc(start)}${end ? ` – ${utc(end)}` : ""} UTC`}</time>;
  }
  const now = new Date();
  const from = new Date(start);
  const first = `${dayName(from, now, t)} ${clock(from, t)}`;
  if (!end) return <time dateTime={start}>{t("statusPage.incidents.since", { time: first })}</time>;
  const to = new Date(end);
  const last = dayDistance(to, from) === 0 ? clock(to, t) : ` ${dayName(to, now, t)} ${clock(to, t)}`;
  return (
    <span>
      <time dateTime={start}>{first}</time>
      {dayDistance(to, from) === 0 ? "–" : " –"}
      <time dateTime={end}>{last}</time>
    </span>
  );
}

/**
 * A moment inside a sentence, such as "son kesinti dün 13:29": the time
 * alone today, the day's word in lower case after that. In UTC, labelled,
 * until the browser knows the visitor's zone.
 */
export function MomentInSentence({ iso }: { iso: string }) {
  const t = useT();
  const mounted = useMounted();
  if (!mounted) return <time dateTime={iso}>{`${iso.slice(0, 10)} ${iso.slice(11, 16)} UTC`}</time>;
  const now = new Date();
  const date = new Date(iso);
  const time = clock(date, t);
  const days = dayDistance(now, date);
  const day = days === 1 ? t("time.yesterday").toLocaleLowerCase(t.intl) : dayName(date, now, t);
  return (
    <time dateTime={iso} title={date.toISOString()}>
      {days === 0 ? time : `${day} ${time}`}
    </time>
  );
}
