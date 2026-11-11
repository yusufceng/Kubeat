import Link from "next/link";
import { duration, uptime as formatUptime } from "@/lib/format";
import type { Outage } from "@/lib/fleet";
import type { SlowRow } from "@/lib/fleet-hero";
import type { StripOutage, StripWindow } from "@/lib/fleet-strip";
import { getT } from "@/lib/i18n/server";
import type { StateCounts } from "@/lib/workspace";
import { ArrowIcon } from "@/app/ui/fleet-icons";
import { FleetStrip } from "@/app/ui/fleet-strip";
import { LiveDuration } from "@/app/ui/live-duration";
import type { ListFilter } from "@/app/ui/shell-state";
import { ShowInList } from "@/app/ui/show-in-list";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";
import { TimeAgo } from "@/app/ui/time-ago";

/** A monitor among the slowest to answer, with its name. */
export type SlowMonitor = SlowRow & { name: string };

/** The outage that ended last among the strip's. */
export type LastOutage = StripOutage & { end: number };

/** The state of a fleet in one sentence, and its glyph. */
function fleetState(counts: StateCounts, t: Awaited<ReturnType<typeof getT>>): { sentence: string; glyph: GlyphKind } {
  const degraded = counts.verifying + counts.recovering + counts.degraded;
  if (counts.down > 0) return { sentence: t.n("monitorsList.hero.down", counts.down), glyph: "down" };
  if (degraded > 0) return { sentence: t.n("monitorsList.hero.degraded", degraded), glyph: "warn" };
  if (counts.up === 0) return { sentence: t("monitorsList.hero.noneActive"), glyph: "paused" };
  if (counts.up === counts.total) return { sentence: t.n("monitorsList.hero.allUp", counts.total), glyph: "up" };
  return { sentence: t.n("monitorsList.hero.upNoneFailing", counts.up), glyph: "up" };
}

/**
 * The head of the overview (and of a namespace's page), every line of it a
 * figure, a state or a way somewhere: the state of the fleet as a sentence
 * with its glyph; its counts by state, each a glyph, a figure and a word,
 * and a button to that part of the list; a line of figures; the slowest
 * answers and the last outage, where there are any; beside them the last
 * hours as a strip; and under a hairline, each outage still going on, with
 * its cause and how long it has lasted. Past outagesLimit the rest are a
 * click away in the list. Nothing here is decoration: the glyphs carry the
 * color.
 */
export async function FleetHero({
  level = 1,
  counts,
  namespaces,
  uptime,
  window,
  checkedAt,
  strip,
  outages,
  outagesLimit,
  slowest,
  lastOutage,
}: {
  /** The heading level of the sentence: 1 on the overview, 2 under a page's own title. */
  level?: 1 | 2;
  counts: StateCounts;
  /** How many namespaces; left out on a namespace's own page. */
  namespaces?: number;
  uptime: number | null;
  /** The period of the uptime, in seconds. */
  window: number;
  checkedAt: string | null;
  strip: {
    outages: StripOutage[];
    windows: StripWindow[];
    now: number;
    hours: number;
    slots: number;
    knownFrom: number;
    fleet: number;
  };
  outages: Outage[];
  outagesLimit: number;
  /** The monitors that answered slowest at their latest check, slowest first; empty leaves the line out. */
  slowest: SlowMonitor[];
  /** The outage that ended last within the strip's hours; null leaves the line out. */
  lastOutage: LastOutage | null;
}) {
  const t = await getT();
  const id = "fl-hero-title"; // one summary per page
  const state = fleetState(counts, t);
  const Heading = level === 1 ? "h1" : "h2";
  const shown = outages.slice(0, outagesLimit);
  const degraded = counts.verifying + counts.recovering + counts.degraded;
  // Idle, as the list's filter counts it: paused, not yet checked, in maintenance.
  const idle = counts.paused + counts.pending + counts.maintenance;
  // The sentence of the uptime and of the last check, each split around the figure it holds.
  const [uptimeBefore, uptimeAfter] = t("monitorsList.overview.status.uptime", { window: duration(window, t) }).split("{uptime}");
  const [checkedBefore, checkedAfter] = t("monitorsList.hero.checked").split("{ago}");

  // A count: the state's glyph, its figure and its word, and the way to those monitors in the list.
  const count = (filter: ListFilter, glyph: GlyphKind, n: number, word: string, tone = "") => (
    <ShowInList
      filter={filter}
      className={`fl-count ${tone} ${n === 0 ? "fl-count-zero" : ""}`}
      title={t("shell.pulse.show", { what: `${n} ${word}` })}
    >
      <StatusGlyph kind={glyph} />
      <b>{n}</b>
      <span>{word}</span>
    </ShowInList>
  );

  return (
    <section className="panel fl-hero" aria-labelledby={id}>
      <div className="fl-hero-top">
        <div className="fl-hero-text">
          <div className="fl-state">
            <StatusGlyph kind={state.glyph} size={12} live={state.glyph === "down"} />
            <Heading id={id} className={level === 1 ? "type-title" : "type-heading"}>
              {state.sentence}
            </Heading>
          </div>
          <div role="group" aria-label={t("monitorsList.hero.counts")} className="fl-counts">
            {count("up", "up", counts.up, t("monitorsList.filter.up"))}
            {count("down", "down", counts.down, t("monitorsList.filter.down"), "fl-count-down")}
            {count("degraded", "warn", degraded, t("monitorsList.filter.degraded"), "fl-count-warn")}
            {count("idle", "paused", idle, t("monitorsList.filter.idle"))}
          </div>
          <p className="fl-sub">
            <span>{t.n("monitorsList.overview.status.monitors", counts.total)}</span>
            {namespaces !== undefined && namespaces > 0 && <span>{t.n("monitorsList.hero.namespaces", namespaces)}</span>}
            {uptime !== null && (
              <span>
                {uptimeBefore}
                <b className="font-semibold text-ink">{formatUptime(uptime, t)}</b>
                {uptimeAfter}
              </span>
            )}
            {checkedAt && (
              <span>
                {checkedBefore}
                <TimeAgo iso={checkedAt} />
                {checkedAfter}
              </span>
            )}
          </p>
          {(slowest.length > 0 || lastOutage) && (
            <dl className="fl-facts">
              {slowest.length > 0 && (
                <>
                  <dt>{t("monitorsList.hero.slowest")}</dt>
                  <dd>
                    {slowest.map((s) => (
                      <span key={s.id} className="fl-fact">
                        {/* Over its own threshold: the warn glyph, named for the screen reader. */}
                        {s.slow && <StatusGlyph kind="warn" label={t("monitorsList.hero.slow")} />}
                        <Link href={`/monitors/${s.id}`}>{s.name}</Link>
                        <span className="fl-fact-ms">{Math.round(s.ms).toLocaleString(t.intl)} ms</span>
                      </span>
                    ))}
                  </dd>
                </>
              )}
              {lastOutage && (
                <>
                  <dt>{t("monitorsList.hero.lastOutage")}</dt>
                  <dd>
                    <span className="fl-fact">
                      <Link href={`/monitors/${lastOutage.monitor}`}>{lastOutage.name}</Link>
                    </span>
                    <span className="fl-fact">
                      <TimeAgo iso={new Date(lastOutage.end).toISOString()} />
                    </span>
                    <span className="fl-fact">
                      {t("monitorsList.hero.lasted", {
                        duration: duration(Math.max(1, Math.round((lastOutage.end - lastOutage.start) / 1000)), t),
                      })}
                    </span>
                  </dd>
                </>
              )}
            </dl>
          )}
        </div>
        <FleetStrip {...strip} />
      </div>
      {outages.length > 0 && (
        <ul className="fl-outages" aria-label={t("monitorsList.overview.open.label")}>
          {shown.map((o) => (
            // The line opens the monitor; its button, the incident's page.
            <li key={o.id} className="fl-outage">
              <StatusGlyph kind="down" live />
              <span className="fl-outage-who">
                <Link href={`/monitors/${o.id}`} className="fl-outage-name">
                  {o.name}
                </Link>
                {o.namespace && <span className="fl-outage-ns">{o.namespace}</span>}
              </span>
              <span className="fl-outage-cause" title={o.cause}>
                {o.cause}
              </span>
              <span className="fl-outage-for">
                <span className="sr-only">{t("monitorsList.hero.lasting")} </span>
                <LiveDuration since={o.since} />
              </span>
              <Link href={`/incidents/${o.incident}`} className="fl-outage-open" title={t("incident.number", { id: o.incident })}>
                {t("monitorsList.hero.incident")}
                <ArrowIcon width={15} height={15} />
              </Link>
            </li>
          ))}
          {outages.length > shown.length && (
            <li>
              <ShowInList filter="down" className="fl-outage-more">
                {t.n("monitorsList.hero.moreOutages", outages.length - shown.length)}
                <ArrowIcon width={15} height={15} />
              </ShowInList>
            </li>
          )}
        </ul>
      )}
    </section>
  );
}
