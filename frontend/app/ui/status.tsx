import Link from "next/link";
import type { Monitor } from "@/lib/api";
import type { T } from "@/lib/i18n/translate";
import { ArrowIcon } from "@/app/ui/fleet-icons";
import { LiveDuration } from "@/app/ui/live-duration";
import { StateChip } from "@/app/ui/state-chip";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

/**
 * What the UI shows for a monitor. It combines the latest check with the
 * incident state: one failed check is not yet an outage, and one successful
 * check does not always end one. degraded is a monitor that answers, but
 * slower than its slow_above_ms for failure_threshold checks in a row.
 */
export type DisplayState =
  | "up"
  | "down"
  | "degraded"
  | "verifying"
  | "recovering"
  | "maintenance"
  | "pending"
  | "paused";

export function displayState(
  m: Pick<Monitor, "status" | "open_incident" | "maintenance_until">,
): DisplayState {
  if (m.status === "paused") return "paused";
  // Answering again (fast or slow) while its outage is still open is recovering.
  if (m.open_incident) return m.status === "up" || m.status === "degraded" ? "recovering" : "down";
  // In maintenance nothing is alerted on, whatever the checks say.
  if (m.maintenance_until && m.status !== "pending") return "maintenance";
  // A failing monitor without an incident is still being verified.
  return m.status === "down" ? "verifying" : m.status;
}

/**
 * How each state is drawn: its glyph (degraded, verifying and recovering
 * share the half-filled one) and the class of its text. label is the
 * message key of its name, shown with t().
 */
export const stateStyles: Record<DisplayState, { glyph: GlyphKind; text: string; label: string }> = {
  up: { glyph: "up", text: "text-up-ink", label: "monitorState.up" },
  down: { glyph: "down", text: "text-down-ink", label: "monitorState.down" },
  degraded: { glyph: "warn", text: "text-warn-ink", label: "monitorState.degraded" },
  verifying: { glyph: "warn", text: "text-warn-ink", label: "monitorState.verifying" },
  recovering: { glyph: "warn", text: "text-warn-ink", label: "monitorState.recovering" },
  maintenance: { glyph: "maint", text: "text-maint-ink", label: "monitorState.maintenance" },
  pending: { glyph: "pending", text: "text-ink-3", label: "monitorState.pending" },
  paused: { glyph: "paused", text: "text-ink-3", label: "monitorState.paused" },
};

/**
 * The badges render on the server and on the client alike, so they are
 * handed the translator of wherever they are: getT() or useT().
 *
 * The state as its glyph and name. live marks the monitor's state right
 * now, so that an ongoing outage pulses; a past event leaves it off.
 */
export function StatusBadge({ state, t, live = false }: { state: DisplayState; t: T; live?: boolean }) {
  const s = stateStyles[state];
  return (
    <span className={`inline-flex items-center gap-2 text-sm font-medium ${s.text}`}>
      <StatusGlyph kind={s.glyph} live={live && state === "down"} />
      {t(s.label)}
    </span>
  );
}

/**
 * The state as a chip, for headers: always the current state, so an outage
 * pulses. With since (an outage's start), the chip counts how long it has
 * lasted. With incident (the open one's id), the chip opens its page and
 * says which it is. note is a few words after the rest, such as what a
 * degraded monitor is slower than.
 */
export function StatusPill({
  state,
  t,
  since,
  incident,
  note,
}: {
  state: DisplayState;
  t: T;
  since?: string;
  incident?: number;
  note?: string;
}) {
  const s = stateStyles[state];
  const name = t(s.label);
  // The duration is a component, so the sentence is split around its placeholder.
  const [before, after] = since ? t("stateChip.for", { state: name }).split("{duration}") : [name, ""];
  const chip = (
    <StateChip kind={s.glyph} live={state === "down"}>
      {before}
      {since && <LiveDuration since={since} />}
      {after}
      {note && ` · ${note}`}
      {incident !== undefined && (
        <span className="mon-chip-to">
          <ArrowIcon width={13} height={13} />
          {t("incident.number", { id: incident })}
        </span>
      )}
    </StateChip>
  );
  if (incident === undefined) return chip;
  return (
    <Link href={`/incidents/${incident}`} className="mon-chip-link" title={t("stateChip.open")}>
      {chip}
    </Link>
  );
}
