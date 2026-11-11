/**
 * The shapes a state takes, one language everywhere a state is shown (the
 * list's rows and its state filter, the hover card, the hero, the chart's
 * legend). Each differs in shape as well as color, so it reads for the
 * colorblind, in print and under a forced-colors scheme, where only the
 * shapes' borders survive:
 *
 *   up       a filled circle, --up
 *   down     a diamond, --down; still going on, it wears the pulsing ring
 *   warn     a half-filled circle, --warn: degraded, verifying, recovering
 *            (and a partly failed bucket or day)
 *   maint    a hatched circle, --maint
 *   paused   a ring with two bars, --ink-3
 *   pending  a dashed ring, --ink-3: no check has run yet
 */
export type GlyphKind = "up" | "down" | "warn" | "maint" | "paused" | "pending";

/** 10 in chips, the filter and the hover card; 12 in the list's rows; 16 in the hero. */
export type GlyphSize = 10 | 12 | 16;

/**
 * A state as its glyph (drawn in globals.css, .glyph). `live` marks an
 * outage that is still going on, and only then does anything move: the
 * diamond wears a ring that swells a little and fades, then rests, on four
 * slow beats (--duration-slow, --ease-out); pending and the rest are still.
 * For viewers who ask for less motion the ring stands still, and the shape
 * and the color say the rest.
 *
 * Pass `label` when no visible text next to the glyph says the state; the
 * glyph then names it to screen readers and on hover. Without a label it
 * is hidden from them, and the text beside it must do the naming (a list
 * row names the state inside its link, in sr-only text).
 */
export function StatusGlyph({
  kind,
  size = 10,
  live = false,
  label,
  className = "",
}: {
  kind: GlyphKind;
  size?: GlyphSize;
  live?: boolean;
  label?: string;
  className?: string;
}) {
  return (
    <span
      className={`glyph glyph-${kind}${live && kind === "down" ? " glyph-live" : ""} ${className}`}
      style={size === 10 ? undefined : ({ "--g": `${size}px` } as React.CSSProperties)}
      {...(label ? { role: "img", "aria-label": label, title: label } : { "aria-hidden": true })}
    />
  );
}

/** The CSS color variables of a glyph's state: the hue, and its text step. */
export const glyphTone: Record<GlyphKind, { color: string; ink: string; soft: string; line: string }> = {
  up: { color: "var(--up)", ink: "var(--up-ink)", soft: "var(--up-soft)", line: "var(--up-line)" },
  down: { color: "var(--down)", ink: "var(--down-ink)", soft: "var(--down-soft)", line: "var(--down-line)" },
  warn: { color: "var(--warn)", ink: "var(--warn-ink)", soft: "var(--warn-soft)", line: "var(--warn-line)" },
  maint: { color: "var(--maint)", ink: "var(--maint-ink)", soft: "var(--maint-soft)", line: "var(--maint-line)" },
  paused: { color: "var(--ink-3)", ink: "var(--ink-2)", soft: "var(--hover)", line: "var(--line-strong)" },
  pending: { color: "var(--ink-3)", ink: "var(--ink-2)", soft: "var(--hover)", line: "var(--line-strong)" },
};
