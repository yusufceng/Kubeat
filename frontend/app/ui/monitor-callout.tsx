import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

export type CalloutTone = "warn" | "down" | "maint" | "muted";

const glyphs: Record<CalloutTone, GlyphKind> = { warn: "warn", down: "down", maint: "maint", muted: "paused" };

/**
 * A line of the page that asks for attention, in its state's soft color
 * with the state's glyph: a monitor nobody hears about, one in maintenance,
 * a certificate close to its end. The text reads on its own; the action,
 * when there is one, sits at its end.
 */
export function Callout({
  tone,
  children,
  action,
  className = "",
}: {
  tone: CalloutTone;
  children: React.ReactNode;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={`mon-callout mon-callout-${tone} ${className}`}>
      <StatusGlyph kind={glyphs[tone]} />
      <p>
        {children}
        {action && <> {action}</>}
      </p>
    </div>
  );
}
