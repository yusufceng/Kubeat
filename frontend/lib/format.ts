/** "12 sn önce", "3 dk önce", ... relative to now. */
export function timeAgo(iso: string, now: number): string {
  const seconds = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (seconds < 60) return `${seconds} sn önce`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} dk önce`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} sa önce`;
  return `${Math.floor(hours / 24)} gün önce`;
}

/** 30 -> "30 sn", 90 -> "1,5 dk", 86400 -> "24 sa". */
export function duration(seconds: number): string {
  const format = (n: number) => n.toLocaleString("tr-TR", { maximumFractionDigits: 1 });
  if (seconds < 60) return `${seconds} sn`;
  if (seconds < 3600) return `${format(seconds / 60)} dk`;
  return `${format(seconds / 3600)} sa`;
}

export function uptime(percent: number | null): string {
  if (percent === null) return "–";
  // Floor so that 99.996% is not shown as a perfect 100%.
  const floored = Math.floor(percent * 100) / 100;
  return `%${floored.toLocaleString("tr-TR", { minimumFractionDigits: 2 })}`;
}

export function dateTime(iso: string): string {
  return new Date(iso).toLocaleString("tr-TR", { timeZoneName: "short" });
}
