import type { Metadata } from "next";
import { Suspense } from "react";
import { getStats, type Incident, listIncidents, listMaintenance, type Maintenance, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import {
  activity,
  fleetUptime,
  lastChecked,
  ongoingOutages,
  readTime,
  stripInputs,
  toFleetItem,
} from "@/lib/fleet";
import { getT } from "@/lib/i18n/server";
import { compareNamespaces } from "@/lib/namespaces";
import { byUrgency } from "@/lib/urgency";
import { countStates, workspaceMonitors } from "@/lib/workspace";
import { ActivityFeed } from "@/app/ui/activity-feed";
import { BackendError } from "@/app/ui/backend-error";
import { ClusterMap, type MapGroup, type MapItem } from "@/app/ui/cluster-map";
import { EmptyFleet } from "@/app/ui/empty-fleet";
import { FleetHero } from "@/app/ui/fleet-hero";
import { certificateReadout, FleetReadouts, mttrReadout, stateReadouts } from "@/app/ui/fleet-readouts";
import { shellUser } from "@/app/ui/session-controls";

/**
 * Titled after how the fleet stands, so that the tab says it: "3 monitors
 * down", "1 monitor degraded", or else the overview's name.
 */
export async function generateMetadata(): Promise<Metadata> {
  const t = await getT();
  try {
    const c = countStates(await workspaceMonitors());
    const degraded = c.verifying + c.recovering;
    if (c.down > 0) return { title: t.n("monitorsList.hero.down", c.down) };
    if (degraded > 0) return { title: t.n("monitorsList.hero.degraded", degraded) };
  } catch {
    // The page says what is wrong.
  }
  return { title: t("monitorsList.overview.title") };
}

/** The monitors by namespace for the cluster map, problems first in each. */
function mapGroups(monitors: Monitor[], locale: string): MapGroup[] {
  const groups = new Map<string, MapItem[]>();
  for (const m of monitors) {
    const { id, name, kindShort, namespace, state, uptime, downSince, error } = toFleetItem(m);
    groups.set(namespace, [...(groups.get(namespace) ?? []), { id, name, kindShort, namespace, state, uptime, downSince, error }]);
  }
  const order = byUrgency<MapItem>(locale);
  return [...groups.entries()]
    .sort(([a], [b]) => compareNamespaces(a, b))
    .map(([namespace, items]) => ({ namespace, items: items.sort(order) }));
}

async function Overview() {
  const t = await getT();
  // Without a session this goes to the sign-in page; unreachable, it is null
  // and the monitors below say what is wrong.
  const me = await shellUser();
  let monitors: Monitor[];
  let incidents: Incident[];
  try {
    [monitors, incidents] = await Promise.all([workspaceMonitors(), listIncidents(config.incidentsLimit)]);
  } catch (err) {
    return (
      <>
        <h1 className="sr-only">{t("monitorsList.overview.title")}</h1>
        <BackendError what={t("monitorsList.overview.what")} err={err} />
      </>
    );
  }
  if (monitors.length === 0) return <EmptyFleet canEdit={me?.can_edit ?? false} />;

  // What only adds to the page; without it the page goes on.
  const [stats, windows] = await Promise.all([
    getStats(config.statsDays).catch(() => null),
    listMaintenance(config.maintenanceLimit).catch((): Maintenance[] => []),
  ]);
  const now = readTime();
  const counts = countStates(monitors);
  const { uptime, window } = fleetUptime(monitors);
  const states = stateReadouts(counts, monitors, windows, t, now);

  return (
    <div className="fl-overview">
      <FleetHero
        counts={counts}
        namespaces={new Set(monitors.map((m) => m.namespace).filter(Boolean)).size}
        uptime={uptime}
        window={window}
        checkedAt={lastChecked(monitors)}
        strip={{ ...stripInputs(monitors, incidents, windows, config.incidentsLimit), now, ...config.fleetStrip }}
        outages={ongoingOutages(monitors)}
        outagesLimit={config.overviewOutages}
      />
      <FleetReadouts
        label={t("monitorsList.readouts.label")}
        items={[...states, certificateReadout(monitors, t), mttrReadout(stats, config.statsDays, t)]}
        phone={[states[0], states[1], states[2], states[4]]}
      />
      <div className="fl-cols">
        <ClusterMap groups={mapGroups(monitors, t.intl)} />
        <ActivityFeed {...activity(incidents, monitors, config.overviewEvents, config.flapWindowSeconds * 1000)} asOf={now} />
      </div>
    </div>
  );
}

/** While the overview is read: its shape, without words. */
function OverviewSkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-hidden>
      <div className="panel h-[138px]" />
      <div className="panel h-[88px]" />
      <div className="fl-cols">
        <div className="panel h-72" />
        <div className="panel h-72" />
      </div>
    </div>
  );
}

export default async function Home() {
  const t = await getT();
  return (
    <Suspense
      fallback={
        <>
          <h1 className="sr-only">{t("monitorsList.overview.title")}</h1>
          <OverviewSkeleton />
        </>
      }
    >
      <Overview />
    </Suspense>
  );
}
