import { span } from "@/lib/format";
import type { T } from "@/lib/i18n/translate";

/**
 * Figures for readouts: the number large, its unit small and muted. They
 * render on the server and in the browser alike, so they take the
 * translator of wherever they are.
 */

/**
 * A percentage with its sign set small, where the language puts it: 99.77%
 * in English, %99,77 in Turkish. Floored, so that 99.996 is not a perfect
 * 100; 100 itself goes without decimals.
 */
export function Percent({ value, t }: { value: number; t: T }) {
  const floored = Math.floor(value * 100) / 100;
  const digits = floored === 100 ? 0 : 2;
  const parts = new Intl.NumberFormat(t.intl, {
    style: "percent",
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).formatToParts(floored / 100);
  return (
    <span className="whitespace-nowrap">
      {parts.map((p, i) =>
        p.type === "percentSign" ? (
          <small key={i} className="fl-unit">
            {p.value}
          </small>
        ) : (
          p.value
        ),
      )}
    </span>
  );
}

/** A length of time as "7 h 52 min" / "7 sa 52 dk", with each unit small. */
export function DurationFigure({ seconds, t }: { seconds: number; t: T }) {
  const text = span(seconds * 1000, t);
  const pieces = [...text.matchAll(/([\d.,]+)\s*([^\d\s]+)/g)];
  if (pieces.length === 0) return <span>{text}</span>;
  return (
    <span className="whitespace-nowrap">
      {pieces.map(([, n, unit], i) => (
        <span key={i}>
          {i > 0 && " "}
          {n}
          <small className="fl-unit">{unit}</small>
        </span>
      ))}
    </span>
  );
}
