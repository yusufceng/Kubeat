import type { Maintenance, Monitor, Stats } from "@/lib/api";
import { certificateSummary } from "@/lib/fleet";
import { span } from "@/lib/format";
import type { T } from "@/lib/i18n/translate";
import type { StateCounts } from "@/lib/workspace";
import { DurationFigure, Percent } from "@/app/ui/figures";
import { type Readout, Readouts } from "@/app/ui/readouts";
import { ShortTime } from "@/app/ui/short-time";
import { displayState } from "@/app/ui/status";

/** A sentence split around the place a component goes, such as a time that only the browser can write. */
function around(text: string, slot: string, node: React.ReactNode): React.ReactNode {
  const [before, after = ""] = text.split(`{${slot}}`);
  return (
    <>
      {before}
      {node}
      {after}
    </>
  );
}

/** The earliest of some moments, as ISO strings; null for none. */
function earliest(moments: (string | null | undefined)[]): string | null {
  return moments.filter((m): m is string => Boolean(m)).sort()[0] ?? null;
}

/**
 * The fleet's states as readouts, in the order of the list's filter: up,
 * down (since when), degraded, maintenance (until when, or the next
 * window), idle (paused, new).
 */
export function stateReadouts(counts: StateCounts, monitors: Monitor[], windows: Maintenance[], t: T, asOf: number): Readout[] {
  const degraded = counts.verifying + counts.recovering;
  const idle = counts.paused + counts.pending;
  const downSince = earliest(monitors.filter((m) => displayState(m) === "down").map((m) => m.open_incident?.started_at));
  const maintenanceEnds = earliest(monitors.filter((m) => displayState(m) === "maintenance").map((m) => m.maintenance_until));
  const nextWindow = earliest(windows.filter((w) => w.state === "upcoming").map((w) => w.starts_at));
  const parts = (list: [number, string][]) =>
    list
      .filter(([n]) => n > 0)
      .map(([n, key]) => t(key, { count: n }))
      .join(" · ");

  return [
    {
      label: t("monitorsList.filter.up"),
      glyph: "up",
      value: counts.up,
      tone: counts.up > 0 ? undefined : "muted",
      hint: t("monitorsList.readouts.of", { total: counts.total }),
    },
    {
      label: t("monitorsList.filter.down"),
      glyph: "down",
      value: counts.down,
      tone: counts.down > 0 ? "down" : "muted",
      hint: downSince ? around(t("monitorsList.readouts.since"), "time", <ShortTime iso={downSince} asOf={asOf} />) : t("monitorsList.readouts.noOutages"),
    },
    {
      label: t("monitorsList.filter.degraded"),
      glyph: "warn",
      value: degraded,
      tone: degraded > 0 ? "warn" : "muted",
      hint:
        degraded > 0
          ? parts([
              [counts.verifying, "monitorsList.readouts.verifying"],
              [counts.recovering, "monitorsList.readouts.recovering"],
            ])
          : t("monitorsList.readouts.noneFailing"),
    },
    {
      label: t("monitorState.maintenance"),
      glyph: "maint",
      value: counts.maintenance,
      tone: counts.maintenance > 0 ? undefined : "muted",
      hint: maintenanceEnds
        ? around(t("monitorsList.readouts.ends"), "time", <ShortTime iso={maintenanceEnds} asOf={asOf} />)
        : nextWindow
          ? around(t("monitorsList.readouts.next"), "time", <ShortTime iso={nextWindow} asOf={asOf} />)
          : t("monitorsList.readouts.nonePlanned"),
    },
    {
      label: t("monitorsList.filter.idle"),
      glyph: "paused",
      value: idle,
      tone: "muted",
      hint:
        idle > 0
          ? parts([
              [counts.paused, "monitorsList.readouts.paused"],
              [counts.pending, "monitorsList.readouts.pending"],
            ])
          : t("monitorsList.readouts.noneIdle"),
    },
  ];
}

/** The certificates the monitors watch: how many the backend warns about, and the soonest to expire. */
export function certificateReadout(monitors: Monitor[], t: T): Readout {
  const c = certificateSummary(monitors);
  return {
    label: t("monitorsList.readouts.certs"),
    value: c.watched > 0 ? c.warned : t("common.none"),
    tone: c.warned > 0 ? "warn" : "muted",
    hint:
      c.soonest === null
        ? t("monitorsList.readouts.noTls")
        : c.soonest < 0
          ? t("monitorsList.readouts.certExpired")
          : t("monitorsList.readouts.soonest", { days: t.n("format.days", c.soonest) }),
  };
}

/** The share of successful checks over the stats' period. */
export function uptimeReadout(stats: Stats | null, days: number, t: T): Readout {
  return {
    label: t("monitorsList.readouts.uptime", { window: t.n("format.days", days) }),
    value: stats?.uptime != null ? <Percent value={stats.uptime} t={t} /> : t("common.none"),
    tone: stats?.uptime != null ? undefined : "muted",
    hint: stats
      ? t.n("monitorsList.readouts.checks", stats.checks, { count: stats.checks.toLocaleString(t.intl) })
      : t("monitorsList.readouts.unavailable"),
  };
}

/** The outages of the stats' period, and how long they lasted together. */
export function outagesReadout(stats: Stats | null, days: number, t: T): Readout {
  return {
    label: t("monitorsList.readouts.outagesIn", { window: t.n("format.days", days) }),
    value: stats ? stats.outages : t("common.none"),
    tone: stats && stats.outages > 0 ? undefined : "muted",
    hint: !stats
      ? t("monitorsList.readouts.unavailable")
      : stats.outages > 0
        ? t("monitorsList.readouts.downtime", { duration: span(stats.downtime_seconds * 1000, t) })
        : t("monitorsList.readouts.noOutages"),
  };
}

/** Mean time to recovery over the stats' period, with the outages it is the mean of. */
export function mttrReadout(stats: Stats | null, days: number, t: T): Readout {
  const window = t.n("format.days", days);
  return {
    label: t("monitorsList.readouts.mttr", { window }),
    value: stats?.mttr_seconds != null ? <DurationFigure seconds={stats.mttr_seconds} t={t} /> : t("common.none"),
    tone: stats?.mttr_seconds != null ? undefined : "muted",
    hint: !stats
      ? t("monitorsList.readouts.unavailable")
      : stats.outages > 0
        ? t.n("monitorsList.readouts.outages", stats.outages)
        : t("monitorsList.readouts.noOutages"),
  };
}

/**
 * The readouts strip, and on a phone the few that matter most in two by
 * two, so that the first screen keeps the summary and the list in view.
 */
export function FleetReadouts({ items, phone, label }: { items: Readout[]; phone: Readout[]; label: string }) {
  return (
    <>
      <Readouts items={items} label={label} className="max-sm:hidden" />
      <Readouts items={phone} label={label} className="sm:hidden" />
    </>
  );
}
