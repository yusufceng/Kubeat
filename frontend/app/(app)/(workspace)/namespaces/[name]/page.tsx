import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import { getStats, type Incident, listMaintenance, type Maintenance, type Monitor, queryIncidents } from "@/lib/api";
import { config } from "@/lib/config";
import { activity, fleetUptime, lastChecked, ongoingOutages, readTime, stripInputs, windowsFor } from "@/lib/fleet";
import { getT } from "@/lib/i18n/server";
import { MANUAL_LABEL, namespaceFromSlug } from "@/lib/namespaces";
import { urgencyRank } from "@/lib/urgency";
import { countStates, workspaceMonitors } from "@/lib/workspace";
import { ActivityFeed } from "@/app/ui/activity-feed";
import { BackendError } from "@/app/ui/backend-error";
import { FleetHero } from "@/app/ui/fleet-hero";
import { FleetReadouts, mttrReadout, outagesReadout, stateReadouts, uptimeReadout } from "@/app/ui/fleet-readouts";
import { MonitorTable } from "@/app/ui/monitor-table";
import { CrumbTrail } from "@/app/ui/nav";
import { Crumbs } from "@/app/ui/shell-state";
import { displayState } from "@/app/ui/status";

/** Titled after the namespace, and what in it is down: "checkout · 1 monitor down". */
export async function generateMetadata({ params }: PageProps<"/namespaces/[name]">): Promise<Metadata> {
  const t = await getT();
  const namespace = namespaceFromSlug((await params).name);
  const label = namespace || t(MANUAL_LABEL);
  try {
    const monitors = (await workspaceMonitors()).filter((m) => m.namespace === namespace);
    if (monitors.length === 0) return { title: t("notFound.namespace.title") };
    const down = countStates(monitors).down;
    return { title: down > 0 ? `${label} · ${t.n("monitorsList.hero.down", down)}` : label };
  } catch {
    return { title: label };
  }
}

/**
 * One namespace (or the monitors added by hand) in the overview's
 * language: its state as a sentence with the strip of its last hours and
 * its outages, its figures over the last days, its monitors as a table and
 * what happened in it lately.
 */
async function Namespace({ params }: { params: Promise<{ name: string }> }) {
  const t = await getT();
  const namespace = namespaceFromSlug((await params).name);
  const label = namespace || t(MANUAL_LABEL);

  let monitors: Monitor[];
  let incidents: Incident[];
  try {
    const [all, listed] = await Promise.all([
      workspaceMonitors(),
      queryIncidents({ namespaces: [namespace], limit: config.incidentsLimit }),
    ]);
    monitors = all.filter((m) => m.namespace === namespace);
    // The backend filters by namespace; this keeps to the monitors shown either way.
    const ids = new Set(monitors.map((m) => m.id));
    incidents = listed.filter((i) => ids.has(i.monitor_id));
  } catch (err) {
    return (
      <>
        <h1 className="sr-only">{label}</h1>
        <BackendError what={t("monitorsList.namespace.what")} err={err} />
      </>
    );
  }
  if (monitors.length === 0) notFound();

  // What only adds to the page; without it the page goes on.
  const [stats, windows] = await Promise.all([
    getStats(config.statsDays, [namespace]).catch(() => null),
    listMaintenance(config.maintenanceLimit).then(
      (w) => windowsFor(w, monitors),
      (): Maintenance[] => [],
    ),
  ]);
  const now = readTime();
  const counts = countStates(monitors);
  const { uptime, window } = fleetUptime(monitors);
  const states = stateReadouts(counts, monitors, windows, t, now);
  // Problems first; otherwise in the order the backend gives.
  const ordered = [...monitors].sort((a, b) => urgencyRank(displayState(a)) - urgencyRank(displayState(b)));
  const overview = { label: t("monitorsList.overview.title"), href: "/" };

  return (
    <>
      <Crumbs items={[overview, { label, mono: namespace !== "" }]} />
      <header className="fl-page-head">
        {/* The context bar carries the breadcrumb on a wide screen; here, the steps above the title. */}
        <CrumbTrail items={[overview]} here={false} className="mb-2 lg:hidden" />
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
          {/* A long name wraps (after its hyphens, as a rule) rather than losing its end. */}
          <h1 className={`type-display min-w-0 [overflow-wrap:anywhere] ${namespace ? "fl-title-mono" : ""}`}>{label}</h1>
          <span className="fl-kind-tag">
            {namespace ? t("monitorsList.namespace.tag.namespace") : t("monitorsList.namespace.tag.group")}
          </span>
        </div>
      </header>
      <FleetHero
        level={2}
        counts={counts}
        uptime={uptime}
        window={window}
        checkedAt={lastChecked(monitors)}
        strip={{ ...stripInputs(monitors, incidents, windows, config.incidentsLimit), now, ...config.fleetStrip }}
        outages={ongoingOutages(monitors)}
        outagesLimit={config.overviewOutages}
      />
      <FleetReadouts
        label={t("monitorsList.readouts.labelOf", { group: label })}
        items={[
          states[0],
          states[1],
          states[2],
          states[4],
          uptimeReadout(stats, config.statsDays, t),
          outagesReadout(stats, config.statsDays, t),
          mttrReadout(stats, config.statsDays, t),
        ]}
        phone={[states[0], states[1], states[2], states[4]]}
      />
      <MonitorTable monitors={ordered} slots={config.listBeats} tones={config.uptimeTones} />
      <ActivityFeed {...activity(incidents, monitors, config.overviewEvents, config.flapWindowSeconds * 1000)} asOf={now} />
    </>
  );
}

export default async function Page({ params }: PageProps<"/namespaces/[name]">) {
  return (
    <Suspense
      fallback={
        <div className="flex flex-col gap-5" aria-hidden>
          <div className="h-[34px] w-56 rounded-sm bg-hover" />
          <div className="panel h-[120px]" />
          <div className="panel h-[88px]" />
          <div className="panel h-64" />
        </div>
      }
    >
      <div className="fl-overview">
        <Namespace params={params} />
      </div>
    </Suspense>
  );
}
