"use client";

import { clock, dayDistance } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { useMounted } from "@/lib/use-mounted";

/** A day near now as people say it: "bugün", "yarın", "dün", or "Çar 22 Eki". */
function dayWords(date: Date, now: Date, t: T): string {
  const days = dayDistance(date, now);
  if (days === 0) return t("time.today");
  if (days === 1) return t("statusPage.when.tomorrow");
  if (days === -1) return t("time.yesterday");
  return date.toLocaleDateString(t.intl, { weekday: "short", day: "numeric", month: "short" });
}

/**
 * A stretch of time as a maintenance window reads: "bugün 22:00 – 23:00",
 * "Cum 24 Eki 01:00 – 02:00", and both ends with their days when it spans
 * two. In the viewer's zone; UTC, said so, until the browser takes over.
 */
export function WhenRange({ from, to }: { from: string; to: string }) {
  const mounted = useMounted();
  const t = useT();
  if (!mounted) {
    return (
      <span className="tabular-nums">
        <time dateTime={from}>{`${from.slice(0, 10)} ${from.slice(11, 16)}`}</time> – <time dateTime={to}>{`${to.slice(0, 10)} ${to.slice(11, 16)}`}</time> UTC
      </span>
    );
  }
  const now = new Date();
  const a = new Date(from);
  const b = new Date(to);
  const sameDay = dayDistance(a, b) === 0;
  return (
    <span className="tabular-nums">
      <time dateTime={from}>{`${dayWords(a, now, t)} ${clock(a, t)}`}</time> – <time dateTime={to}>{sameDay ? clock(b, t) : `${dayWords(b, now, t)} ${clock(b, t)}`}</time>
    </span>
  );
}
