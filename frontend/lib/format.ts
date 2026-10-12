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

/** 30 -> "30 sn", 90 -> "1,5 dk", 86400 -> "24 sa", 604800 -> "7 gün". */
export function duration(seconds: number): string {
  const format = (n: number) => n.toLocaleString("tr-TR", { maximumFractionDigits: 1 });
  if (seconds < 60) return `${seconds} sn`;
  if (seconds < 3600) return `${format(seconds / 60)} dk`;
  if (seconds >= 2 * 86400 && seconds % 86400 === 0) return `${seconds / 86400} gün`;
  return `${format(seconds / 3600)} sa`;
}

export function uptime(percent: number | null): string {
  if (percent === null) return "–";
  // Floor so that 99.996% is not shown as a perfect 100%.
  const floored = Math.floor(percent * 100) / 100;
  return `%${floored.toLocaleString("tr-TR", { minimumFractionDigits: 2 })}`;
}

/** Length of a period: 6000 -> "6 sn", 200000 -> "3 dk 20 sn", 9000000 -> "2 sa 30 dk". */
export function span(ms: number): string {
  const seconds = Math.max(0, Math.round(ms / 1000));
  if (seconds < 60) return `${seconds} sn`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} dk ${seconds % 60} sn`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} sa ${minutes % 60} dk`;
  return `${Math.floor(hours / 24)} gün ${hours % 24} sa`;
}

/** CPU in Kubernetes units: 0.25 -> "250m", 1.5 -> "1,5", 0.0002 -> "<1m". */
export function cpu(cores: number): string {
  if (cores > 0 && cores < 0.0005) return "<1m"; // in use, but would round to zero
  if (cores < 1) return `${Math.round(cores * 1000)}m`;
  return cores.toLocaleString("tr-TR", { maximumFractionDigits: 2 });
}

/** Memory in binary units: 104857600 -> "100 Mi". */
export function memory(bytes: number): string {
  const mi = bytes / 2 ** 20;
  if (mi < 1024) return `${Math.round(mi)} Mi`;
  return `${(mi / 1024).toLocaleString("tr-TR", { maximumFractionDigits: 1 })} Gi`;
}
