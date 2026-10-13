import type { T } from "@/lib/i18n/translate";

/** "12 sn önce" / "12 s ago", "3 dk önce" / "3 min ago", ... relative to now. */
export function timeAgo(iso: string, now: number, t: T): string {
  const seconds = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  const ago = (duration: string) => t("format.ago", { duration });
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

/** Length of a period: 6000 -> "6 sn" / "6 s", 200000 -> "3 dk 20 sn" / "3 min 20 s", 9000000 -> "2 sa 30 dk" / "2 h 30 min". */
export function span(ms: number, t: T): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return t("format.seconds", { count: seconds });
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${t("format.minutes", { count: minutes })} ${t("format.seconds", { count: seconds % 60 })}`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${t("format.hours", { count: hours })} ${t("format.minutes", { count: minutes % 60 })}`;
  return `${t.n("format.days", Math.floor(hours / 24))} ${t("format.hours", { count: hours % 24 })}`;
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
