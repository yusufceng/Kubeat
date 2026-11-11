import { type GlyphKind, glyphTone, StatusGlyph } from "@/app/ui/status-glyph";

/** One figure of a readouts row. */
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
 * Figures side by side in one row, split by hairlines, in no box: a label
 * in small capitals with its glyph, the value in the large tabular face, a
 * hint. Where the row is narrow the figures wrap onto more rows (polish.css,
 * .rd).
 */
export function Readouts({ items, label, className = "" }: { items: Readout[]; label?: string; className?: string }) {
  return (
    <dl aria-label={label} className={`rd ${className}`}>
      {items.map((r) => (
        <div key={r.label} className="rd-item">
          <dt className="rd-label type-label">
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
            className="rd-value type-kpi"
            style={{ color: r.tone === "muted" ? "var(--ink-3)" : r.tone ? glyphTone[r.tone].ink : undefined }}
          >
            {r.value}
            {r.unit && <small className="rd-unit">{r.unit}</small>}
          </dd>
          {r.hint && <dd className="rd-hint">{r.hint}</dd>}
        </div>
      ))}
    </dl>
  );
}
