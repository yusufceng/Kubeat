/**
 * The shapes a state takes. Each differs in shape as well as color, so it
 * reads for the colorblind and in print: up is a filled circle, down a
 * diamond, degraded (verifying, recovering, partial) a half-filled circle,
 * maintenance a hatched one, paused a ring with two bars, pending a dashed
 * ring.
 */
export type GlyphKind = "up" | "down" | "warn" | "maint" | "paused" | "pending";

export type GlyphSize = 10 | 12 | 16;

/**
 * A state as its glyph (drawn in globals.css, .glyph). `live` marks an
 * outage that is still going on: the diamond wears a pulsing ring, which
 * stands still for viewers who ask for less motion.
 *
 * Pass `label` when no visible text next to the glyph says the state; the
 * glyph then names it to screen readers and on hover. Without a label it
 * is hidden from them, and the text beside it must do the naming.
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
