"use client";

import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { useMounted } from "@/lib/use-mounted";

/*
 * Times as the incident pages write them: on a 24-hour clock, in the
 * viewer's zone. The server cannot know that zone, so the first render is
 * in UTC, which the day headings and titles say, until the browser takes
 * over (as app/ui/local-time.tsx does).
 */

/** The calendar day a moment falls on, as a key: in the viewer's zone once mounted, else in UTC. */
export function dayKey(iso: string, local: boolean): string {
  if (!local) return iso.slice(0, 10);
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** "12:57" or "12:57:14", in the viewer's zone or in UTC. */
export function clock(iso: string, t: T, local: boolean, seconds = true): string {
  if (!local) return iso.slice(11, seconds ? 19 : 16);
  return new Date(iso).toLocaleTimeString(t.intl, {
    hour: "2-digit",
    minute: "2-digit",
    ...(seconds && { second: "2-digit" }),
    hourCycle: "h23",
  });
}

/** Whole calendar days from one moment's day to another's, in the viewer's zone. */
function daysBetween(later: Date, earlier: Date): number {
  const day = (d: Date) => Date.UTC(d.getFullYear(), d.getMonth(), d.getDate());
  return Math.round((day(later) - day(earlier)) / 86_400_000);
}

/**
 * The day of a group of the timeline: "Bugün", "Dün", "18 Ekim Pazar",
 * with the year once it is not this one; in UTC, said so, until mounted.
 */
export function DayLabel({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  const d = new Date(iso);
  // The server's words for a date might not be the browser's to the letter,
  // so the first render keeps to digits.
  if (!mounted) return <time dateTime={iso.slice(0, 10)}>{`${iso.slice(0, 10)} · UTC`}</time>;
  const now = new Date();
  const days = daysBetween(now, d);
  const text =
    days === 0
      ? t("time.today")
      : days === 1
        ? t("time.yesterday")
        : d.toLocaleDateString(t.intl, {
            weekday: "long",
            day: "numeric",
            month: "long",
            ...(d.getFullYear() !== now.getFullYear() && { year: "numeric" }),
          });
  return <time dateTime={dayKey(iso, true)}>{text}</time>;
}

/** A moment of the timeline's time column: "12:57:14". */
export function Clock({ iso, seconds = true }: { iso: string; seconds?: boolean }) {
  const mounted = useMounted();
  const t = useT();
  return (
    <time dateTime={iso} title={mounted ? new Date(iso).toISOString() : undefined}>
      {clock(iso, t, mounted, seconds)}
    </time>
  );
}

/**
 * When something began, as short as a table cell wants it: "12:57" today,
 * "Pzt 12:57" within the week, "14 Eki 12:57" this year, "14 Eki 2025"
 * before. The whole moment is in the title.
 */
export function CompactTime({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  const d = new Date(iso);
  let text = `${iso.slice(0, 10)} ${iso.slice(11, 16)} UTC`;
  if (mounted) {
    const now = new Date();
    const days = daysBetween(now, d);
    const time = clock(iso, t, true, false);
    if (days === 0) text = time;
    else if (days > 0 && days < 7) text = `${d.toLocaleDateString(t.intl, { weekday: "short" })} ${time}`;
    else if (d.getFullYear() === now.getFullYear())
      text = `${d.toLocaleDateString(t.intl, { day: "numeric", month: "short" })} ${time}`;
    else text = d.toLocaleDateString(t.intl, { day: "numeric", month: "short", year: "numeric" });
  }
  return (
    <time
      dateTime={iso}
      title={
        mounted
          ? d.toLocaleString(t.intl, { dateStyle: "full", timeStyle: "medium", hourCycle: "h23" })
          : undefined
      }
    >
      {text}
    </time>
  );
}

/**
 * A moment with its day, for the incident's header: "12:57:14" today,
 * "Dün 12:57:14", "18 Eki 12:57:14", "18 Eki 2025 12:57:14".
 */
export function Moment({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  const d = new Date(iso);
  let text = `${iso.slice(0, 10)} ${iso.slice(11, 19)} UTC`;
  if (mounted) {
    const now = new Date();
    const days = daysBetween(now, d);
    const time = clock(iso, t, true);
    if (days === 0) text = `${t("time.today")} ${time}`;
    else if (days === 1) text = `${t("time.yesterday")} ${time}`;
    else
      text = `${d.toLocaleDateString(t.intl, {
        day: "numeric",
        month: "short",
        ...(d.getFullYear() !== now.getFullYear() && { year: "numeric" }),
      })} ${time}`;
  }
  return (
    <time dateTime={iso} title={mounted ? d.toISOString() : undefined}>
      {text}
    </time>
  );
}

/** A moment to the minute with its date, for the ends of a period: "13 Eki 18:00", with the year once it is not this one. */
export function DateTime({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  if (!mounted) return <time dateTime={iso}>{`${iso.slice(0, 10)} ${iso.slice(11, 16)} UTC`}</time>;
  const d = new Date(iso);
  const date = d.toLocaleDateString(t.intl, {
    day: "numeric",
    month: "short",
    ...(d.getFullYear() !== new Date().getFullYear() && { year: "numeric" }),
  });
  return (
    <time dateTime={iso} title={d.toISOString()}>
      {`${date} ${clock(iso, t, true, false)}`}
    </time>
  );
}

/** The day of a moment in a word or two: "Bugün", "Dün", "18 Eki", "18 Eki 2025". */
export function DayWord({ iso }: { iso: string }) {
  const mounted = useMounted();
  const t = useT();
  if (!mounted) return <time dateTime={iso}>{`${iso.slice(0, 10)} UTC`}</time>;
  const d = new Date(iso);
  const now = new Date();
  const days = daysBetween(now, d);
  const text =
    days === 0
      ? t("time.today")
      : days === 1
        ? t("time.yesterday")
        : d.toLocaleDateString(t.intl, {
            day: "numeric",
            month: "short",
            ...(d.getFullYear() !== now.getFullYear() && { year: "numeric" }),
          });
  return <time dateTime={dayKey(iso, true)}>{text}</time>;
}

/**
 * A stretch of time within a day, "12:57:44–13:09:14"; across days, both
 * ends with their dates.
 */
export function ClockRange({ from, to }: { from: string; to: string }) {
  const mounted = useMounted();
  const t = useT();
  const sameDay = dayKey(from, mounted) === dayKey(to, mounted);
  const end = (iso: string) =>
    sameDay
      ? clock(iso, t, mounted)
      : `${mounted ? new Date(iso).toLocaleDateString(t.intl, { day: "numeric", month: "short" }) : iso.slice(0, 10)} ${clock(iso, t, mounted)}`;
  return (
    <span className="tabular-nums">
      <time dateTime={from}>{end(from)}</time>–<time dateTime={to}>{end(to)}</time>
    </span>
  );
}
