/** Below these, uptime is shown in amber and in red (WARN_UPTIME_BELOW, CRITICAL_UPTIME_BELOW). */
export type UptimeTones = { warnBelow: number; criticalBelow: number };

/**
 * The color class of an uptime figure: quiet when it is fine, so that
 * only trouble stands out. Floored like the figure, so that 99.896 is
 * amber under a 99.9 line.
 */
export function uptimeTone(value: number | null, tones: UptimeTones): string {
  if (value === null) return "text-ink-3";
  const floored = Math.floor(value * 100) / 100;
  return floored < tones.criticalBelow ? "text-down-ink" : floored < tones.warnBelow ? "text-warn-ink" : "text-ink-3";
}
