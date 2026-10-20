/*
 * Figures as the incident and report pages set them: a period in its
 * largest sensible unit, a percent split so that its sign can be set
 * small, and how an uptime is coloured. No imports, so that the tests run
 * it as it is.
 */

export type DurationUnit = "s" | "min" | "h" | "d";

/**
 * A period as one figure in its largest sensible unit, for a readout:
 * 45 s, 7.5 min, 3.4 h, 12 d. One decimal below ten, whole numbers above;
 * hours go on to two days, so that "36 h" is not "1.5 d".
 */
export function largestUnit(seconds: number): { value: number; unit: DurationUnit } {
  const s = Math.max(0, seconds);
  const round = (n: number) => (n < 10 ? Math.round(n * 10) / 10 : Math.round(n));
  if (s < 60) return { value: Math.round(s), unit: "s" };
  if (s < 3600) return { value: round(s / 60), unit: "min" };
  if (s < 2 * 86400) return { value: round(s / 3600), unit: "h" };
  return { value: round(s / 86400), unit: "d" };
}

/**
 * A percent as its parts in the locale's order, the sign apart: English
 * "99.73%", Turkish "%99,73". Floored at two decimals, so that 99.996 is
 * not shown as a perfect 100.
 */
export function percentParts(percent: number, intl: string): { text: string; sign: boolean }[] {
  const floored = Math.floor(percent * 100) / 100;
  return new Intl.NumberFormat(intl, { style: "percent", minimumFractionDigits: 2, maximumFractionDigits: 2 })
    .formatToParts(floored / 100)
    .map((p) => ({ text: p.value, sign: p.type === "percentSign" }));
}

/**
 * How an uptime is coloured: not at all while it is healthy, amber below
 * 99.9, red below 99; muted when nothing was checked.
 */
export function uptimeTone(percent: number | null): "down" | "warn" | "muted" | null {
  if (percent === null) return "muted";
  if (percent < 99) return "down";
  if (percent < 99.9) return "warn";
  return null;
}
