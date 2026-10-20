import { type GlyphKind, glyphTone, StatusGlyph } from "@/app/ui/status-glyph";

/**
 * A state as a chip, for headers: the glyph and what the state is, on the
 * state's soft color with a hairline of it. children is the text, such as
 * "Down" or "Down for 20 min 46 s" with a live duration in it.
 */
export function StateChip({
  kind,
  live = false,
  children,
  className = "",
}: {
  kind: GlyphKind;
  live?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  const tone = glyphTone[kind];
  return (
    <span
      className={`inline-flex h-6 items-center gap-[7px] whitespace-nowrap rounded-sm px-[9px] text-[12.5px] leading-none font-semibold tabular-nums ${className}`}
      style={{ background: tone.soft, color: tone.ink, boxShadow: `inset 0 0 0 1px ${tone.line}` }}
    >
      <StatusGlyph kind={kind} live={live} />
      {/* One flex item, so that the words and a live duration keep their spaces. */}
      <span>{children}</span>
    </span>
  );
}
