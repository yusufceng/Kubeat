import Link from "next/link";
import { duration, uptime as formatUptime } from "@/lib/format";
import type { Outage } from "@/lib/fleet";
import type { StripOutage, StripWindow } from "@/lib/fleet-strip";
import { getT } from "@/lib/i18n/server";
import type { StateCounts } from "@/lib/workspace";
import { ArrowIcon } from "@/app/ui/fleet-icons";
import { FleetStrip } from "@/app/ui/fleet-strip";
import { LiveDuration } from "@/app/ui/live-duration";
import { ShowInList } from "@/app/ui/show-in-list";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";
import { TimeAgo } from "@/app/ui/time-ago";

/** The state of a fleet in one sentence, its glyph, and the color of its aura. */
function fleetState(counts: StateCounts, t: Awaited<ReturnType<typeof getT>>): { sentence: string; glyph: GlyphKind; tone: string } {
  const degraded = counts.verifying + counts.recovering + counts.degraded;
  if (counts.down > 0) return { sentence: t.n("monitorsList.hero.down", counts.down), glyph: "down", tone: "down" };
  if (degraded > 0) return { sentence: t.n("monitorsList.hero.degraded", degraded), glyph: "warn", tone: "warn" };
  if (counts.up === 0) return { sentence: t("monitorsList.hero.noneActive"), glyph: "paused", tone: "idle" };
  if (counts.up === counts.total) return { sentence: t.n("monitorsList.hero.allUp", counts.total), glyph: "up", tone: "up" };
  return { sentence: t.n("monitorsList.hero.upNoneFailing", counts.up), glyph: "up", tone: "up" };
}

/**
 * The head of the overview (and of a namespace's page): the state of the
 * fleet as a sentence in the display face, over an aura of its color; a
 * line of figures; the last hours as a strip; and below a hairline, each
 * outage that is going on, with its cause and how long it has lasted.
 * Past outagesLimit the rest are a click away in the list.
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
}) {
  const t = await getT();
  const id = "fl-hero-title"; // one summary per page
  const state = fleetState(counts, t);
  const Heading = level === 1 ? "h1" : "h2";
  const shown = outages.slice(0, outagesLimit);
  // The sentence of the uptime and of the last check, each split around the figure it holds.
  const [uptimeBefore, uptimeAfter] = t("monitorsList.overview.status.uptime", { window: duration(window, t) }).split("{uptime}");
  const [checkedBefore, checkedAfter] = t("monitorsList.hero.checked").split("{ago}");

  return (
    <section className="panel fl-hero" data-tone={state.tone} aria-labelledby={id}>
      <div className="fl-hero-top">
        <div className="fl-hero-text">
          <div className="fl-state">
            <StatusGlyph kind={state.glyph} size={16} live={state.glyph === "down"} />
            {/* A size down where the page is narrow (the overview is a container). */}
            <Heading id={id} className={level === 1 ? "type-display-l @max-md:type-display-s" : "type-display-s"}>
              {state.sentence}
            </Heading>
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
