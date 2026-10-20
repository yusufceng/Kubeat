import { cache } from "react";
import { type ClusterEvent, getWorkload, type Pod, type Workload } from "@/lib/api";
import { cpu, memory } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { DayTime } from "@/app/ui/local-time";
import { Callout } from "@/app/ui/monitor-callout";
import { Age } from "@/app/ui/monitor-time";
import { StatusGlyph } from "@/app/ui/status-glyph";

/**
 * The pods behind a monitor, read live from the cluster once per render:
 * the panel and the "pods ready" readout share the answer.
 */
export const readWorkload = cache((monitorId: number) => getWorkload(monitorId));

/**
 * The pods behind a discovered monitor, read live from the cluster. A check
 * through a Service reaches one pod at a time; this shows the rest.
 */
export async function PodsPanel({ monitorId }: { monitorId: number }) {
  const t = await getT();
  let workload: Workload;
  try {
    workload = await readWorkload(monitorId);
  } catch {
    return (
      <PodsFrame t={t}>
        <p className="px-4 pt-2 pb-4 text-[13px] text-ink-3">{t("monitorPage.pods.unreadable")}</p>
      </PodsFrame>
    );
  }
  return <PodsView workload={workload} t={t} />;
}

/** The panel while the cluster is being read. */
export function PodsLoading({ t }: { t: T }) {
  return (
    <PodsFrame t={t}>
      <p className="px-4 pt-2 pb-4 text-[13px] text-ink-3">{t("common.readingCluster")}</p>
    </PodsFrame>
  );
}

function PodsFrame({ t, sub, children }: { t: T; sub?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="panel mon-pods min-w-0" aria-labelledby="mon-pods-title">
      <div className="mon-ph">
        <h2 id="mon-pods-title">{t("monitorPage.pods.title")}</h2>
        {sub && <span className="mon-sub">{sub}</span>}
      </div>
      {children}
    </section>
  );
}

/** What the cluster says about the pods: a mini table, then its recent events. */
export function PodsView({ workload, t }: { workload: Workload; t: T }) {
  if (!workload.available) {
    return (
      <PodsFrame t={t}>
        <div className="px-4 pt-2 pb-4 text-[13px]">
          <p className="text-ink-2">{t("monitorPage.pods.unavailable")}</p>
          {workload.reason && <p className="mon-detail mt-1.5">{workload.reason}</p>}
        </div>
      </PodsFrame>
    );
  }
  const allReady = workload.ready === workload.total;
  const service = workload.service ? <span className="mon-mono text-ink-2">svc/{workload.service}</span> : null;
  if (workload.total === 0) {
    // The service's name is markup, so the sentence is split around its placeholder.
    const [before, after] = t("monitorPage.pods.noneBehindService").split("{service}");
    return (
      <PodsFrame t={t} sub={service}>
        <div className="px-4 pt-3 pb-4">
          <Callout tone="down">
            {workload.service ? (
              <>
                {before}
                <span className="mon-mono">{workload.service}</span>
                {after}
              </>
            ) : (
              t("monitorPage.pods.noneInWorkload")
            )}
          </Callout>
        </div>
        <Events workload={workload} t={t} />
      </PodsFrame>
    );
  }

  return (
    <PodsFrame
      t={t}
      sub={
        <>
          <span className={allReady ? "text-up-ink" : "font-medium text-warn-ink"}>
            {t.n("monitorPage.pods.ready", workload.total, { ready: workload.ready })}
          </span>
          {service && <> · {service}</>}
        </>
      }
    >
      <div className="overflow-x-auto px-1 pt-2">
        <table className="mon-table">
          <thead>
            <tr>
              <th>{t("monitorPage.pods.col.pod")}</th>
              <th>{t("monitorPage.pods.col.status")}</th>
              <th className="num">{t("monitorPage.pods.col.ready")}</th>
              <th className="num">{t("monitorPage.pods.col.restarts")}</th>
              <th className="mon-pods-node">{t("monitorPage.pods.col.node")}</th>
            </tr>
          </thead>
          <tbody>
            {workload.pods.map((p) => (
              <PodRow key={p.name} pod={p} metrics={workload.metrics.available} t={t} />
            ))}
          </tbody>
        </table>
      </div>
      {!workload.metrics.available && (
        <p className="mon-help px-4">{t("monitorPage.pods.noMetrics", { reason: workload.metrics.reason ?? "" })}</p>
      )}
      <Events workload={workload} t={t} />
    </PodsFrame>
  );
}

function PodRow({ pod: p, metrics, t }: { pod: Pod; metrics: boolean; t: T }) {
  const usage = [
    p.cpu_cores !== null && `${cpu(p.cpu_cores, t)} CPU`,
    p.memory_bytes !== null && memory(p.memory_bytes, t),
  ].filter(Boolean);
  // A pod that receives traffic is up; one that does not is down, with the
  // reason the cluster gives (CrashLoopBackOff, Pending) as its words.
  const state = p.ready ? (p.status === "Running" ? t("monitorPage.pods.readyState") : p.status) : p.status || t("monitorPage.pods.notReady");
  return (
    <tr>
      <td>
        <span className="mon-mono block text-ink">{p.name}</span>
        <span className="block text-xs whitespace-normal text-ink-3">
          {/* Where the panel is narrow, the node joins this line instead of having a column. */}
          {p.node && <span className="mon-pods-node-inline mon-mono">{p.node} · </span>}
          {p.started_at ? <Age since={p.started_at} /> : "–"}
          {metrics && usage.length > 0 && <> · {usage.join(" · ")}</>}
        </span>
      </td>
      <td>
        <span className={`inline-flex items-center gap-2 font-medium ${p.ready ? "text-up-ink" : "text-down-ink"}`}>
          <StatusGlyph kind={p.ready ? "up" : "down"} />
          {state}
        </span>
      </td>
      <td className="num">
        {p.ready_containers}/{p.containers}
      </td>
      <td className={`num ${p.restarts > 0 ? "font-medium text-down-ink" : ""}`}>{p.restarts}</td>
      <td className="mon-pods-node mon-mono text-ink-3">{p.node || "–"}</td>
    </tr>
  );
}

/** What the cluster recorded about the pods lately: the "why" behind a restart or a pod that will not start. */
function Events({ workload, t }: { workload: Workload; t: T }) {
  if (workload.events_reason) {
    return <p className="mon-help px-4 pb-4">{t("monitorPage.pods.eventsUnreadable", { reason: workload.events_reason })}</p>;
  }
  if (workload.events.length === 0) {
    return <p className="mon-help px-4 pb-4">{t("monitorPage.pods.noEvents")}</p>;
  }
  return (
    <div className="px-4 pt-4 pb-3">
      <h3 className="mon-h3">{t("monitorPage.pods.events")}</h3>
      <EventList events={workload.events} />
    </div>
  );
}

/** Cluster events, one per row: when, why (a warning in amber), about what, and the message. */
export function EventList({ events }: { events: ClusterEvent[] }) {
  return (
    <ul className="text-[13px] leading-[18px]">
      {events.map((e, i) => (
        <li key={`${e.at}-${e.object}-${e.reason}-${i}`} className="border-t border-line py-2 first:border-t-0">
          <p className="flex flex-wrap items-baseline gap-x-2 text-xs">
            <span className="text-ink-3 tabular-nums">
              <DayTime iso={e.at} />
            </span>
            <span className={`font-semibold ${e.type === "Warning" ? "text-warn-ink" : "text-ink-2"}`}>
              {e.reason}
              {e.count > 1 && <span className="font-normal text-ink-3"> ×{e.count}</span>}
            </span>
            <span className="mon-mono text-ink-3">{e.object}</span>
          </p>
          <p className="mt-0.5 break-words text-ink-2">{e.message}</p>
        </li>
      ))}
    </ul>
  );
}
