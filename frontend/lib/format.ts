import type { T } from "@/lib/i18n/translate";

/** "12 sn önce" / "12 s ago", "3 dk önce" / "3 min ago", ... relative to now; "şimdi" / "just now" under a second. */
export function timeAgo(iso: string, now: number, t: T): string {
  const seconds = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  const ago = (duration: string) => t("format.ago", { duration });
  if (seconds < 1) return t("format.justNow");
  if (seconds < 60) return ago(t("format.seconds", { count: seconds }));
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return ago(t("format.minutes", { count: minutes }));
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return ago(t("format.hours", { count: hours }));
  return ago(t.n("format.days", Math.floor(hours / 24)));
}

/** 30 -> "30 sn" / "30 s", 90 -> "1,5 dk" / "1.5 min", 86400 -> "24 sa" / "24 h", 604800 -> "7 gün" / "7 days". */
export function duration(seconds: number, t: T): string {
  const format = (n: number) => n.toLocaleString(t.intl, { maximumFractionDigits: 1 });
  if (seconds < 60) return t("format.seconds", { count: seconds });
  if (seconds < 3600) return t("format.minutes", { count: format(seconds / 60) });
  if (seconds >= 2 * 86400 && seconds % 86400 === 0) return t.n("format.days", seconds / 86400);
  return t("format.hours", { count: format(seconds / 3600) });
}

/** 99.996 -> "%99,99" / "99.99%"; null -> "–". */
export function uptime(percent: number | null, t: T): string {
  if (percent === null) return t("common.none");
  // Floor so that 99.996% is not shown as a perfect 100%.
  const floored = Math.floor(percent * 100) / 100;
  return t("format.percent", { value: floored.toLocaleString(t.intl, { minimumFractionDigits: 2 }) });
}

/** Two digits, for the smaller unit of a span: 5 -> "05", as a clock would. */
const twoDigits = (n: number) => String(n).padStart(2, "0");

/**
 * Length of a period in its largest unit and the one below it, the way a
 * table writes it: 6000 -> "6 sn" / "6s", 200000 -> "3 dk 20 sn" /
 * "3m 20s", 3900000 -> "1 sa 05 dk" / "1h 05m", 482400000 -> "5 gün 14 sa"
 * / "5d 14h". A smaller unit that is zero is left out (120000 -> "2 dk" /
 * "2m"), and nothing is ever "0 sn": under half a second it is "<1 sn" /
 * "<1s".
 */
export function span(ms: number, t: T): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds === 0) return t("format.span.lessThanSecond");
  if (seconds < 60) return t("format.span.s", { count: seconds });
  const both = (larger: string, smaller: string, rest: number) => (rest === 0 ? larger : `${larger} ${smaller}`);
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    const rest = seconds % 60;
    return both(t("format.span.m", { count: minutes }), t("format.span.s", { count: twoDigits(rest) }), rest);
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const rest = minutes % 60;
    return both(t("format.span.h", { count: hours }), t("format.span.m", { count: twoDigits(rest) }), rest);
  }
  const rest = hours % 24;
  return both(t("format.span.d", { count: Math.floor(hours / 24) }), t("format.span.h", { count: rest }), rest);
}

/** CPU in Kubernetes units: 0.25 -> "250m", 1.5 -> "1,5" / "1.5", 0.0002 -> "<1m". */
export function cpu(cores: number, t: T): string {
  if (cores > 0 && cores < 0.0005) return "<1m"; // in use, but would round to zero
  if (cores < 1) return `${Math.round(cores * 1000)}m`;
  return cores.toLocaleString(t.intl, { maximumFractionDigits: 2 });
}

/** Memory in binary units: 104857600 -> "100 Mi". */
export function memory(bytes: number, t: T): string {
  const mi = bytes / 2 ** 20;
  if (mi < 1024) return `${Math.round(mi)} Mi`;
  return `${(mi / 1024).toLocaleString(t.intl, { maximumFractionDigits: 1 })} Gi`;
}

/** A response time: 0 -> "<1 ms" (it took some time, just not a whole millisecond), 1234 -> "1.234 ms" / "1,234 ms". */
export function milliseconds(ms: number, t: T): string {
  return ms < 1 ? "<1 ms" : `${Math.round(ms).toLocaleString(t.intl)} ms`;
}

/** Whole calendar days from b to a, in the zone the code runs in (the viewer's, in a browser). */
export function dayDistance(a: Date, b: Date): number {
  const day = (d: Date) => Date.UTC(d.getFullYear(), d.getMonth(), d.getDate());
  return Math.round((day(a) - day(b)) / 86_400_000);
}

/** The time of day on a 24-hour clock: "19:11", in Turkish and English alike. */
export function clock(date: Date, t: T): string {
  return date.toLocaleTimeString(t.intl, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
}

/**
 * A moment as a table row writes it, in as few characters as stay clear:
 * "19:11" on the day of now, "Çar 19:11" / "Wed 19:11" within a week of
 * it either way, "14 Eki 19:11" / "Oct 14 19:11" further off, and with the
 * year once it is not now's. Read in the zone the code runs in, so call it
 * in the browser.
 */
export function compactTime(date: Date, now: Date, t: T): string {
  const time = clock(date, t);
  const days = Math.abs(dayDistance(date, now));
  if (days === 0) return time;
  if (days < 7) return `${date.toLocaleDateString(t.intl, { weekday: "short" })} ${time}`;
  const sameYear = date.getFullYear() === now.getFullYear();
  const day = date.toLocaleDateString(t.intl, { day: "numeric", month: "short", ...(!sameYear && { year: "numeric" }) });
  return `${day} ${time}`;
}
