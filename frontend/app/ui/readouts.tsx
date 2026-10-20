import { type GlyphKind, glyphTone, StatusGlyph } from "@/app/ui/status-glyph";

/** One figure of a readouts panel. */
export type Readout = {
  label: string;
  /** What an abbreviated label stands for ("MTTR"), shown on hover. */
  abbr?: string;
  /** The state the figure counts, drawn before the label. */
  glyph?: GlyphKind;
  value: React.ReactNode;
  /** A unit set small and muted after the value: "ms", "%". */
  unit?: string;
  /** One line under the value: what it covers, or since when. */
  hint?: React.ReactNode;
  /** Colors the value in a state's text color; "muted" for a zero or an absent value. */
  tone?: GlyphKind | "muted";
};

/**
 * Figures side by side in one panel, in columns split by hairlines: the
 * label with its glyph, the value in the large tabular style, a hint. The
 * columns wrap onto more rows where the panel is narrow, and the cells of
 * a shorter last row share its width, so that no row ends in a hole. The
 * hairlines follow, since each cell draws its own left and top line and
 * the panel clips those on its edges.
 */
export function Readouts({ items, label, className = "" }: { items: Readout[]; label?: string; className?: string }) {
  return (
    <dl aria-label={label} className={`panel flex flex-wrap overflow-hidden ${className}`}>
      {items.map((r) => (
        <div key={r.label} className="-mt-px -ml-px min-w-0 flex-[1_1_132px] border-t border-l border-line px-4 pt-3.5 pb-3">
          <dt className="type-caption flex items-center gap-1.5 text-ink-3">
            {r.glyph && <StatusGlyph kind={r.glyph} />}
            {r.abbr ? (
              <abbr title={r.abbr} className="cursor-help truncate underline decoration-dotted underline-offset-2">
                {r.label}
              </abbr>
            ) : (
              <span className="truncate">{r.label}</span>
            )}
          </dt>
          <dd
            className="type-kpi mt-1.5 truncate"
            style={{ color: r.tone === "muted" ? "var(--ink-3)" : r.tone ? glyphTone[r.tone].ink : undefined }}
          >
            {r.value}
            {/* leading-none keeps the small unit from deepening the line. */}
            {r.unit && <small className="ml-0.5 text-[13px] leading-none font-[550] text-ink-3 [font-stretch:100%]">{r.unit}</small>}
          </dd>
          {r.hint && <dd className="mt-0.5 truncate text-xs text-ink-3">{r.hint}</dd>}
        </div>
      ))}
    </dl>
  );
}
