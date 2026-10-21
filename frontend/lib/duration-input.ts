/**
 * A length of time as a form takes it: a count and a unit, kept in whole
 * seconds. The channel form's delay and repeat use it.
 */
export type DurationUnit = "s" | "m" | "h" | "d";

export const durationUnits: DurationUnit[] = ["s", "m", "h", "d"];

export const unitSeconds: Record<DurationUnit, number> = { s: 1, m: 60, h: 3600, d: 86400 };

/**
 * The largest unit that divides the seconds evenly, so that a stored value
 * reads back as it was entered: 600 -> 10 m, 3600 -> 1 h, 90 -> 90 s.
 * Zero is 0 in minutes, the unit most values take.
 */
export function splitSeconds(seconds: number): { count: number; unit: DurationUnit } {
  if (seconds <= 0) return { count: 0, unit: "m" };
  for (const unit of [...durationUnits].reverse()) {
    if (seconds % unitSeconds[unit] === 0) return { count: seconds / unitSeconds[unit], unit };
  }
  return { count: seconds, unit: "s" };
}

/** The seconds a typed count and unit stand for; null when the count is not a whole number of 0 or more. */
export function joinSeconds(count: string, unit: DurationUnit): number | null {
  const n = Number(count);
  if (count.trim() === "" || !Number.isInteger(n) || n < 0) return null;
  return n * unitSeconds[unit];
}
