"use client";

import { useT } from "@/lib/i18n/client";
import { useMounted } from "@/lib/use-mounted";

/** Whole calendar days from one moment to another, in the viewer's zone. */
function dayDistance(a: Date, b: Date): number {
  const day = (d: Date) => Date.UTC(d.getFullYear(), d.getMonth(), d.getDate());
  return Math.round((day(a) - day(b)) / 86_400_000);
}

/**
 * A moment in as few characters as will do, for the hint under a readout,
 * on a 24-hour clock in the viewer's zone: "19:11" today, "Fri 19:11"
 * within a week either way, "16 Oct" beyond. asOf is the moment it is
 * measured from (when the data was read). The server cannot know the
 * viewer's zone, so the first render shows UTC, labelled as such.
 */
export function ShortTime({ iso, asOf }: { iso: string; asOf: number }) {
  const t = useT();
  const mounted = useMounted();
  const date = new Date(iso);
  if (!mounted) return <time dateTime={iso}>{`${iso.slice(11, 16)} UTC`}</time>;
  const days = Math.abs(dayDistance(date, new Date(asOf)));
  const time = date.toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const text =
    days === 0
      ? time
      : days < 7
        ? `${date.toLocaleDateString(t.intl, { weekday: "short" })} ${time}`
        : date.toLocaleDateString(t.intl, { day: "numeric", month: "short" });
  return (
    <time dateTime={iso} title={date.toLocaleString(t.intl, { hourCycle: "h23" })}>
      {text}
    </time>
  );
}
