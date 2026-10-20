"use client";

import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";

/**
 * A moment shown in the viewer's own time zone. The server cannot know that
 * zone, so the first render shows UTC, labelled as such, and the browser
 * replaces it once the component mounts.
 */
export function LocalTime({ iso, seconds = true }: { iso: string; seconds?: boolean }) {
  const mounted = useMounted();
  const t = useT();

  const date = new Date(iso);
  const text = mounted
    ? date.toLocaleString(t.intl, {
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        ...(seconds && { second: "2-digit" }),
      })
    : `${iso.slice(0, 10)} ${iso.slice(11, seconds ? 19 : 16)} UTC`;

  return (
    <time dateTime={iso} title={mounted ? date.toISOString() : undefined}>
      {text}
    </time>
  );
}

/** Whole calendar days between two moments, in the viewer's zone. */
function daysBetween(later: Date, earlier: Date): number {
  const day = (d: Date) => Date.UTC(d.getFullYear(), d.getMonth(), d.getDate());
  return Math.round((day(later) - day(earlier)) / 86_400_000);
}

/**
 * A moment as people say it, on a 24-hour clock in the viewer's zone:
 * "Today 19:11" (or "19:11" with todayWord off), "Yesterday 08:02",
 * "Wed 14 Oct 14:29", and with the year once it is not this one. Like
 * LocalTime, the first render shows UTC until the browser takes over.
 */
export function DayTime({ iso, todayWord = true }: { iso: string; todayWord?: boolean }) {
  const mounted = useMounted();
  const t = useT();

  const date = new Date(iso);
  let text = `${iso.slice(0, 10)} ${iso.slice(11, 16)} UTC`;
  if (mounted) {
    const now = new Date();
    const time = date.toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
    const days = daysBetween(now, date);
    if (days === 0) text = todayWord ? `${t("time.today")} ${time}` : time;
    else if (days === 1) text = `${t("time.yesterday")} ${time}`;
    else if (date.getFullYear() === now.getFullYear())
      text = `${date.toLocaleDateString(t.intl, { weekday: "short", day: "numeric", month: "short" })} ${time}`;
    else text = `${date.toLocaleDateString(t.intl, { day: "numeric", month: "short", year: "numeric" })} ${time}`;
  }

  return (
    <time dateTime={iso} title={mounted ? date.toISOString() : undefined}>
      {text}
    </time>
  );
}
