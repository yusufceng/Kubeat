"use client";

import { useEffect, useState } from "react";
import type { Incident } from "@/lib/api";
import { span } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import { outagesWithin } from "@/lib/monitor-page";
import { useMounted } from "@/lib/use-mounted";

/** The clock, read in the browser and brought up to date every `every` ms; null until mounted. */
function useNow(every: number): number | null {
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => {
    const update = () => setNow(Date.now());
    update();
    const id = setInterval(update, every);
    return () => clearInterval(id);
  }, [every]);
  return now;
}

/**
 * The outages of the last `seconds`, up to now: "kesinti yok", or "2
 * kesinti · 3 sa 5 dk" with how long they kept the monitor down within the
 * period. The clock is the browser's, so the page needs none of its own.
 */
export function OutagesWithin({
  incidents,
  seconds,
}: {
  incidents: Pick<Incident, "kind" | "started_at" | "resolved_at">[];
  seconds: number;
}) {
  const t = useT();
  const now = useNow(60_000);
  if (now === null) return <span aria-hidden>&nbsp;</span>;
  const { count, downMs } = outagesWithin(incidents, now - seconds * 1000, now);
  return (
    // It may take two lines where the readouts are narrow, rather than lose its end.
    <span className="block whitespace-normal">
      {count === 0
        ? t("monitorPage.readout.noOutages")
        : t.n("monitorPage.readout.outages", count, { duration: span(downMs, t) })}
    </span>
  );
}

/**
 * How old something is, such as a pod: "40 sn", "46 dk", "5 sa 12 dk",
 * "5 gün 5 sa". Seconds only while it is younger than a minute, so the
 * figure does not tick for nothing; it is brought up to date every 30 s.
 */
export function Age({ since }: { since: string }) {
  const t = useT();
  const now = useNow(30_000);
  if (now === null) return <span>…</span>;
  const ms = Math.max(0, now - Date.parse(since));
  // Under an hour, whole minutes: span() would add the seconds.
  const text = ms >= 60_000 && ms < 3_600_000 ? t("format.minutes", { count: Math.floor(ms / 60_000) }) : span(ms, t);
  return (
    <time dateTime={since} title={new Date(since).toISOString()}>
      {text}
    </time>
  );
}

/*
 * Times as the monitor page writes them, always on a 24-hour clock in the
 * viewer's zone. The server cannot know that zone, so the first render
 * shows UTC, labelled as such, until the browser takes over (as LocalTime
 * does).
 */

const clock = (d: Date, intl: string, seconds = false) =>
  d.toLocaleTimeString(intl, { hour: "2-digit", minute: "2-digit", hourCycle: "h23", ...(seconds && { second: "2-digit" }) });

/** "14 Eki" this year, "14 Eki 2025" before. */
const date = (d: Date, intl: string) =>
  d.toLocaleDateString(intl, {
    day: "numeric",
    month: "short",
    ...(d.getFullYear() !== new Date().getFullYear() && { year: "numeric" }),
  });

/**
 * When a check ran, to the second: "16:09:02" today, "20 Eki 16:09:02"
 * before.
 */
export function CheckTime({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  const d = new Date(iso);
  let text = `${iso.slice(11, 19)} UTC`;
  if (mounted) {
    const today = new Date().toDateString() === d.toDateString();
    text = today ? clock(d, t.intl, true) : `${date(d, t.intl)} ${clock(d, t.intl, true)}`;
  }
  return (
    <time dateTime={iso} title={mounted ? d.toISOString() : undefined} className="tabular-nums">
      {text}
    </time>
  );
}

/**
 * A stretch of time, as short as it reads unambiguously: "14 Eki
 * 14:29–14:54" within a day, "14 Eki 14:29 – 16 Eki 19:11" across days.
 * Without an end it runs until now, which `now` words.
 */
export function TimeRange({ from, to, now }: { from: string; to: string | null; now?: string }) {
  const mounted = useMounted();
  const t = useT();
  const a = new Date(from);
  const b = to ? new Date(to) : null;
  if (!mounted) {
    return (
      <span className="tabular-nums">
        {from.slice(0, 16).replace("T", " ")} – {to ? to.slice(0, 16).replace("T", " ") : now} UTC
      </span>
    );
  }
  const end = !b ? now : a.toDateString() === b.toDateString() ? clock(b, t.intl) : `${date(b, t.intl)} ${clock(b, t.intl)}`;
  return (
    <span className="tabular-nums" title={`${a.toISOString()} – ${b ? b.toISOString() : ""}`}>
      {date(a, t.intl)} {clock(a, t.intl)}
      {b && a.toDateString() === b.toDateString() ? "–" : " – "}
      {end}
    </span>
  );
}

/** A calendar date alone: "14 Oca 2027", with the year always. */
export function CalendarDate({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  const d = new Date(iso);
  return (
    <time dateTime={iso} title={mounted ? d.toISOString() : undefined}>
      {mounted ? d.toLocaleDateString(t.intl, { day: "numeric", month: "short", year: "numeric" }) : iso.slice(0, 10)}
    </time>
  );
}
