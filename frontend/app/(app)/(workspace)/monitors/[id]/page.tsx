import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { cache, Suspense } from "react";
import {
  ApiError,
  type CheckResult,
  getGrid,
  getMonitor,
  getMonitorChannels,
  getSeries,
  getSeriesBetween,
  getUptime,
  type Incident,
  listIncidents,
  listMaintenance,
  listResults,
  type Maintenance,
  type Monitor,
  type MonitorChannel,
  type Series,
  type UptimeWindow,
} from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { duration } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { kubectlName, maintenanceSpans, upSince, uptimeTone } from "@/lib/monitor-page";
import { typeLabel } from "@/lib/monitor-types";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { BeatStrip } from "@/app/ui/beat-strip";
import { CheckGrid } from "@/app/ui/check-grid";
import { ChevronIcon, EditIcon } from "@/app/ui/icons";
import { LatencyChart, type Span } from "@/app/ui/latency-chart";
import { DayTime } from "@/app/ui/local-time";
import { Callout } from "@/app/ui/monitor-callout";
import { NoChannelsCallout } from "@/app/ui/monitor-channels";
import { CheckRows } from "@/app/ui/monitor-checks";
import { WhyPanel } from "@/app/ui/monitor-diagnosis";
import { Percent } from "@/app/ui/monitor-figures";
import { MonitorHeader } from "@/app/ui/monitor-header";
import { ZoomInIcon } from "@/app/ui/monitor-icons";
import { IncidentTimeline } from "@/app/ui/monitor-incidents";
import { monitorLabel } from "@/app/ui/monitor-name";
import { CalendarDate, OutagesWithin, TimeRange } from "@/app/ui/monitor-time";
import { PodsLoading, PodsPanel, readWorkload } from "@/app/ui/pods-panel";
import { PushUrl } from "@/app/ui/push-url";
import { type Readout, Readouts } from "@/app/ui/readouts";
import { Crumbs } from "@/app/ui/shell-state";
import { displayState, stateStyles } from "@/app/ui/status";
import { TimeAgo } from "@/app/ui/time-ago";
import { WindowPanel } from "@/app/ui/window-panel";

// How much of a timeframe's length the zoomed chart shows on each side of
// it, and the least it shows.
const ZOOM_MARGIN = 0.25;
const ZOOM_MARGIN_MIN_MS = 60_000;

/** The monitor, read once per request for both the page and its title. */
const readMonitor = cache((id: number) => getMonitor(id));

/** Titled after the monitor and how it stands, "pricing · Down", so that its tab says so. */
export async function generateMetadata({ params }: PageProps<"/monitors/[id]">): Promise<Metadata> {
  const id = Number((await params).id);
  const t = await getT();
  if (!Number.isInteger(id) || id <= 0) return { title: t("notFound.monitor.title") };
  try {
    const monitor = await readMonitor(id);
    return { title: `${monitorLabel(monitor).name} · ${t(stateStyles[displayState(monitor)].label)}` };
  } catch (err) {
    return err instanceof ApiError && err.status === 404 ? { title: t("notFound.monitor.title") } : {};
  }
}

/** The spans the check-history grid offers: a day of short slots, a week of hours. */
const historyRanges = [
  { key: "24h", hours: 24 },
  { key: "7d", hours: 168 },
];

/** The grid of the check history; read on its own so the page does not wait for it. */
async function HistoryGrid({ monitorId, hours, inspectBase }: { monitorId: number; hours: number; inspectBase: string }) {
  const t = await getT();
  let grid;
  try {
    grid = await getGrid(monitorId, hours);
  } catch {
    return <p className="text-[13px] text-ink-3">{t("monitorPage.history.gridError")}</p>;
  }
  return <CheckGrid grid={grid} inspectBase={inspectBase} />;
}

/** A query value as a moment, or null when it is not one. */
function moment(v: string | string[] | undefined): Date | null {
  if (typeof v !== "string") return null;
  const at = new Date(v);
  return Number.isNaN(at.getTime()) ? null : at;
}

/** The selection as query parameters, to carry it across the range links. */
function selectionQuery(selection: Span | null): string {
  if (!selection) return "";
  let q = `&from=${encodeURIComponent(new Date(selection.from).toISOString())}`;
  if (selection.to !== null) q += `&to=${encodeURIComponent(new Date(selection.to).toISOString())}`;
  return q;
}

/** One setting of the check: its name, its value, and whether the value is code-like (set in mono). */
type Setting = [string, React.ReactNode, boolean?];

/** The settings of the check, as label and value pairs. */
function checkSettings(m: Monitor, t: T): Setting[] {
  const rows: Setting[] = [
    [t("monitorPage.settings.type"), t(typeLabel(m.type))],
    [
      t(m.type === "push" ? "monitorPage.settings.reportInterval" : "monitorPage.settings.checkInterval"),
      duration(m.interval_seconds, t),
    ],
    [t(m.type === "push" ? "monitorPage.settings.grace" : "monitorPage.settings.timeout"), duration(m.timeout_seconds, t)],
    [t("monitorPage.settings.threshold"), t.n("monitorPage.settings.threshold.value", m.failure_threshold)],
  ];
  if (m.retry_interval_seconds > 0) {
    rows.push([t("monitorPage.settings.retryInterval"), duration(m.retry_interval_seconds, t)]);
  }
  if (m.inverted) rows.push([t("monitorPage.settings.inverted"), t("monitorPage.settings.inverted.value")]);

  const tls = (skip: boolean) => t(skip ? "monitorPage.settings.tls.unverified" : "monitorPage.settings.tls.verified");
  if (m.http) {
    const h = m.http;
    rows.push([t("monitorPage.settings.method"), h.method, true], [t("monitorPage.settings.expectedStatus"), h.expected_status, true]);
    rows.push([
      t("monitorPage.settings.redirects"),
      h.max_redirects === 0
        ? t("monitorPage.settings.redirects.none")
        : t("monitorPage.settings.redirects.max", { count: h.max_redirects }),
    ]);
    if (h.auth) {
      rows.push([t("monitorPage.settings.auth"), h.auth === "basic" ? `Basic (${h.auth_user})` : "Bearer token"]);
    }
    if (h.keyword) {
      rows.push([
        t(h.invert_keyword ? "monitorPage.settings.keyword.absent" : "monitorPage.settings.keyword.present"),
        h.keyword,
        true,
      ]);
    }
    if (h.json_path) {
      rows.push([t("monitorPage.settings.jsonCondition"), `${h.json_path} ${h.json_operator} ${h.json_expected}`, true]);
    }
    const headers = Object.keys(h.headers);
    if (headers.length > 0) rows.push([t("monitorPage.settings.headers"), headers.join(", "), true]);
    if (h.body) {
      rows.push([
        t("monitorPage.settings.body"),
        t.n("monitorPage.settings.body.value", h.body.length, {
          type: h.content_type || t("monitorPage.settings.body.noType"),
        }),
      ]);
    }
    if (h.tls_skip_verify) rows.push([t("monitorPage.settings.tlsCert"), tls(true)]);
  }
  if (m.tcp?.tls) rows.push(["TLS", t("monitorPage.settings.tcpTls", { verified: tls(m.tcp.tls_skip_verify) })]);
  if (m.dns) {
    rows.push([t("monitorPage.settings.recordType"), m.dns.record_type, true]);
    rows.push(
      m.dns.resolver
        ? [t("monitorPage.settings.resolver"), m.dns.resolver, true]
        : [t("monitorPage.settings.resolver"), t("monitorPage.settings.resolver.default")],
    );
    if (m.dns.expected) rows.push([t("monitorPage.settings.expected"), m.dns.expected, true]);
  }
  if (m.grpc) {
    rows.push(
      m.grpc.service
        ? [t("monitorPage.settings.service"), m.grpc.service, true]
        : [t("monitorPage.settings.service"), t("monitorPage.settings.service.whole")],
    );
    rows.push([
      "TLS",
      m.grpc.tls ? t("monitorPage.settings.tlsOn", { verified: tls(m.grpc.tls_skip_verify) }) : t("monitorPage.settings.tlsOff"),
    ]);
  }
  if (m.database) {
    const d = m.database;
    if (d.username) rows.push([t("monitorPage.settings.user"), d.username, true]);
    rows.push([
      t("monitorPage.settings.password"),
      t(d.password ? "monitorPage.settings.password.set" : "monitorPage.settings.password.none"),
    ]);
    if (d.database) rows.push([t("monitorPage.settings.database"), d.database, true]);
    if (d.query) rows.push([t("monitorPage.settings.query"), d.query, true]);
    rows.push([
      "TLS",
      d.tls ? t("monitorPage.settings.tlsOn", { verified: tls(d.tls_skip_verify) }) : t("monitorPage.settings.tlsOff"),
    ]);
  }
  if (m.workload) {
    rows.push([
      t("monitorPage.settings.minReady"),
      m.workload.min_ready > 0
        ? t.n("monitorPage.settings.minReady.atLeast", m.workload.min_ready)
        : t("monitorPage.settings.minReady.all"),
    ]);
  }
  if (m.websocket) {
    if (m.websocket.subprotocol) rows.push([t("monitorPage.settings.subprotocol"), m.websocket.subprotocol, true]);
    if (m.websocket.tls_skip_verify) rows.push([t("monitorPage.settings.tlsCert"), tls(true)]);
  }
  return rows;
}

async function MonitorDetail({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const id = Number((await params).id);
  if (!Number.isInteger(id) || id <= 0) notFound();

  const query = await searchParams;
  // A span selected on the chart, or named by a notification's link; its
  // end is open (now) when absent.
  const selectedFrom = moment(query.from);
  const selectedTo = selectedFrom ? moment(query.to) : null;
  let selection: Span | null =
    selectedFrom && (!selectedTo || selectedTo > selectedFrom)
      ? { from: selectedFrom.getTime(), to: selectedTo?.getTime() ?? null }
      : null;
  // Only the offered ranges are accepted; anything else falls back to the default.
  const history = historyRanges.find((h) => h.key === query.history) ?? historyRanges[0];
  const requested = Number(query.range);
  const range = config.chartRanges.includes(requested) ? requested : config.chartRanges[0];

  const me = await currentUser();
  const t = await getT();
  let monitor: Monitor;
  let results: CheckResult[];
  let incidents: Incident[];
  let windows: UptimeWindow[];
  let series: Series;
  let maintenance: Maintenance[];
  let channels: MonitorChannel[] | null;
  try {
    let ranged: Series | null;
    [monitor, results, incidents, windows, ranged, maintenance, channels] = await Promise.all([
      readMonitor(id),
      listResults(id, Math.max(config.historyLimit, config.resultsLimit)),
      listIncidents(config.incidentsLimit, id),
      getUptime(id, config.uptimeWindows),
      // A timeframe has its own series, read below once its end is known.
      selection ? null : getSeries(id, range),
      // Only the chart's bands need these; the page does without them.
      listMaintenance(config.maintenanceLimit).catch(() => []),
      getMonitorChannels(id).catch(() => null),
    ]);
    // A link to an outage that has since ended ("down" messages carry no
    // end) is framed on the incident, not on everything since.
    if (selection && selection.to === null) {
      const from = selection.from;
      const ended = incidents.find((i) => i.kind === "down" && new Date(i.started_at).getTime() === from && i.resolved_at);
      if (ended?.resolved_at) selection = { from, to: new Date(ended.resolved_at).getTime() };
    }
    if (selection) {
      // The chart zooms into the timeframe with a margin on each side, so
      // that what led up to it and what followed are in view; an end past
      // now is cut to now by the backend, which is what an open one gets.
      const margin =
        selection.to === null ? ZOOM_MARGIN_MIN_MS : Math.max((selection.to - selection.from) * ZOOM_MARGIN, ZOOM_MARGIN_MIN_MS);
      series = await getSeriesBetween(
        id,
        new Date(selection.from - margin).toISOString(),
        selection.to === null ? undefined : new Date(selection.to + margin).toISOString(),
      );
    } else {
      series = ranged!;
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return <BackendError what={t("monitorPage.what")} err={err} />;
  }
  const state = displayState(monitor);
  const last = monitor.last_result;
  const failing = monitor.enabled && last !== null && !last.up;
  const hasPods = Boolean(monitor.source) || monitor.type === "workload";
  const object = kubectlName(monitor.source);
  const base = `/monitors/${monitor.id}?range=${range}&history=${history.key}`;
  // The chart frames an incident: from its first failed check to its end, or to now.
  const frame = (i: Incident) =>
    `${base}&from=${encodeURIComponent(i.started_at)}${i.resolved_at ? `&to=${encodeURIComponent(i.resolved_at)}` : ""}#chart`;

  // How long the present state has lasted: an outage since it began, a
  // good run since the last outage ended.
  const since =
    state === "down" && monitor.open_incident
      ? monitor.open_incident.started_at
      : state === "up"
        ? upSince(monitor, incidents)
        : undefined;

  // Days left on the certificate as of the latest check, so the page renders
  // the same on the server and needs no clock of its own.
  const cert = last?.cert_expires_at
    ? {
        expiresAt: last.cert_expires_at,
        daysLeft: Math.floor(
          (new Date(last.cert_expires_at).getTime() - new Date(last.checked_at).getTime()) / 86_400_000,
        ),
      }
    : null;
  // The date is a component, so the sentence is split around its placeholder.
  const maintenanceText = t("monitorPage.maintenance").split("{until}");
  // The outages, with only what the browser needs to count them per period.
  const outages = incidents
    .filter((i) => i.kind === "down")
    .map(({ kind, started_at, resolved_at }) => ({ kind, started_at, resolved_at }));

  const readouts: Readout[] = [
    {
      label: t(monitor.type === "push" ? "monitorPage.readout.reported" : "monitorPage.readout.response"),
      value: last?.up ? (last.latency_ms < 1 ? "<1" : Math.round(last.latency_ms).toLocaleString(t.intl)) : t("common.none"),
      unit: last?.up ? "ms" : undefined,
      tone: last?.up ? undefined : "muted",
      hint: t(!last ? "monitorPage.readout.noChecks" : last.up ? "monitorPage.readout.lastCheck" : "monitorPage.readout.lastFailed"),
    },
    {
      label: t("monitorPage.readout.avg"),
      value: monitor.uptime_percent ? Math.round(monitor.avg_latency_ms).toLocaleString(t.intl) : t("common.none"),
      unit: monitor.uptime_percent ? "ms" : undefined,
      tone: monitor.uptime_percent ? undefined : "muted",
      hint: t("monitorPage.readout.last", { duration: duration(monitor.window_seconds, t) }),
    },
    ...windows.map(
      (w): Readout => ({
        label: t("monitorPage.readout.uptime", { duration: duration(w.window_seconds, t) }),
        value: w.uptime_percent === null ? t("common.none") : <Percent value={w.uptime_percent} intl={t.intl} />,
        tone: w.uptime_percent === null ? "muted" : (uptimeTone(w.uptime_percent) ?? undefined),
        // Counted in the browser, up to its now: the page keeps no clock.
        hint: <OutagesWithin incidents={outages} seconds={w.window_seconds} />,
      }),
    ),
  ];
  if (hasPods) {
    readouts.push({
      label: t("monitorPage.readout.pods"),
      value: (
        <Suspense fallback="…">
          <PodsReadoutValue monitorId={monitor.id} />
        </Suspense>
      ),
      hint: object ?? monitor.target,
    });
  }
  if (cert) {
    readouts.push({
      label: t("monitorPage.readout.cert"),
      value: cert.daysLeft < 0 ? t("monitorPage.readout.certExpired") : cert.daysLeft.toLocaleString(t.intl),
      unit: cert.daysLeft < 0 ? undefined : t.n("monitorPage.readout.days", cert.daysLeft),
      tone: cert.daysLeft < 0 ? "down" : monitor.cert_warning ? "warn" : undefined,
      hint: <CalendarDate iso={cert.expiresAt} />,
    });
  }

  const incidentsPanel = (
    <section className="panel min-w-0" aria-labelledby="mon-incidents-title">
      <div className="mon-ph">
        <h2 id="mon-incidents-title">{t("monitorPage.incidents.title")}</h2>
        {incidents.length > 0 && <span className="mon-sub">{t.n("monitorPage.incidents.count", incidents.length)}</span>}
      </div>
      {incidents.length === 0 ? (
        <p className="px-4 pt-2 pb-4 text-[13px] text-ink-3">{t("monitorPage.incidents.empty")}</p>
      ) : (
        <IncidentTimeline incidents={incidents} shown={config.monitorIncidents} hrefFor={frame} t={t} />
      )}
    </section>
  );
  const settingsPanel = (wide: boolean) => (
    <section className="panel min-w-0" aria-labelledby="mon-settings-title">
      <div className="mon-ph">
        <h2 id="mon-settings-title">{t("monitorPage.settings.title")}</h2>
        {me.can_edit && (
          <span className="mon-ph-end">
            <Link href={`/monitors/${monitor.id}/edit`} className="btn btn-ghost btn-sm">
              <EditIcon width={14} height={14} />
              {t("common.edit")}
            </Link>
          </span>
        )}
      </div>
      <dl className={`mon-dl ${wide ? "mon-dl-2" : ""}`}>
        {checkSettings(monitor, t).map(([name, value, mono]) => (
          <div key={name}>
            <dt>{name}</dt>
            <dd className={mono ? "mon-mono" : "tabular-nums"}>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );

  return (
    <div className="mon">
      <Crumbs
        items={[
          { label: t("monitorPage.breadcrumb.overview"), href: "/" },
          { label: monitor.namespace || t(MANUAL_LABEL), href: namespaceHref(monitor.namespace), mono: monitor.namespace !== "" },
          { label: monitorLabel(monitor).name },
        ]}
      />

      <MonitorHeader
        monitor={monitor}
        state={state}
        since={since}
        channels={channels}
        canEdit={me.can_edit}
        silence={monitor.enabled && !monitor.maintenance_until ? config.maintenanceDurations : null}
        t={t}
      />

      {(!monitor.enabled || (monitor.enabled && monitor.maintenance_until) || monitor.cert_warning || channels?.length === 0) && (
        <div className="-mt-1.5 flex flex-col gap-2">
          {!monitor.enabled && <Callout tone="muted">{t("monitorPage.paused")}</Callout>}
          {monitor.enabled && monitor.maintenance_until && (
            <Callout tone="maint" action={<Link href="/maintenance">{t("monitorPage.maintenance.windows")}</Link>}>
              {maintenanceText[0]}
              <DayTime iso={monitor.maintenance_until} todayWord={false} />
              {maintenanceText[1]}
            </Callout>
          )}
          {monitor.cert_warning && (
            <Callout tone="warn">
              {t("monitorPage.certWarning")}
              {cert && cert.daysLeft >= 0 && `: ${t.n("monitorPage.certWarning.daysLeft", cert.daysLeft)}`}.
            </Callout>
          )}
          {channels?.length === 0 && <NoChannelsCallout canEdit={me.can_edit} t={t} />}
        </div>
      )}

      <section aria-label={t("monitorPage.recentChecks")} className="panel p-4">
        <BeatStrip
          beats={results
            .slice(0, config.historyLimit)
            .toReversed()
            .map((r) => ({
              state: r.up ? "ok" : "down",
              at: new Date(r.checked_at).getTime(),
              text: r.up ? `${Math.round(r.latency_ms).toLocaleString(t.intl)} ms` : t("monitorPage.heartbeat.failed"),
              detail: r.up ? undefined : (r.error ?? undefined),
            }))}
          slots={config.historyLimit}
          label={t("monitorPage.heartbeat.summary", {
            shown: Math.min(results.length, config.historyLimit),
            failed: results.slice(0, config.historyLimit).filter((r) => !r.up).length,
          })}
        />
        <p className="mon-strip-cap">
          <span>{t.n("monitorPage.strip.ago", config.historyLimit)}</span>
          <span>
            {t.n(monitor.type === "push" ? "monitorPage.strip.push" : "monitorPage.strip.every", monitor.failure_threshold, {
              interval: duration(monitor.interval_seconds, t),
            })}
          </span>
          <span>
            {last ? (
              <>
                {t("monitorPage.strip.lastCheck")} <TimeAgo iso={last.checked_at} />
              </>
            ) : (
              t("monitorPage.strip.noChecks")
            )}
          </span>
        </p>
        {monitor.enabled && last?.up && last.detail && <p className="mon-detail">{last.detail}</p>}
      </section>

      {/* Without diagnostics (a push or workload check, an older backend)
          the error beside the state chip is all there is to say. */}
      {failing && last.diagnostics && <WhyPanel result={last} inverted={monitor.inverted} t={t} />}

      <Readouts items={readouts} label={t("monitorPage.readouts")} />

      {monitor.push_token && <PushUrl monitorId={monitor.id} token={monitor.push_token} internalBase={config.pushInternalUrl} />}

      <section id="chart" className="panel scroll-mt-4" aria-labelledby="mon-chart-title">
        <div className="mon-ph">
          <h2 id="mon-chart-title">{t("monitorPage.chart.title")}</h2>
          <span className="mon-sub">{t("monitorPage.chart.subtitle", { duration: duration(series.bucket_seconds, t) })}</span>
          <nav aria-label={t("monitorPage.chart.range")} className="mon-ph-end mon-seg">
            {config.chartRanges
              .toSorted((a, b) => a - b)
              .map((r) => (
                <Link
                  key={r}
                  // Choosing a range leaves the zoomed timeframe.
                  href={`/monitors/${monitor.id}?range=${r}&history=${history.key}`}
                  scroll={false}
                  aria-current={r === range && !selection ? "true" : undefined}
                >
                  {duration(r, t)}
                </Link>
              ))}
          </nav>
        </div>
        {selection && (
          <p className="mx-4 mt-3 flex flex-wrap items-center gap-x-2.5 gap-y-1 rounded-sm bg-accent/8 px-3 py-2 text-xs">
            <ZoomInIcon width={14} height={14} className="text-accent-text" />
            <span className="text-ink-2">{t("monitorPage.chart.zoomed")}</span>
            <span className="font-medium">
              <TimeRange
                from={new Date(selection.from).toISOString()}
                to={selection.to === null ? null : new Date(selection.to).toISOString()}
                now={t("monitorPage.chart.zoomed.now")}
              />
            </span>
            <Link href={base} scroll={false} className="ml-auto font-medium text-accent-text hover:underline">
              {t("monitorPage.chart.zoomOut", { duration: duration(range, t) })}
            </Link>
          </p>
        )}
        <div className="px-4 pt-3 pb-4">
          <LatencyChart
            series={series}
            baseHref={base}
            selection={selection}
            // Only outages: a click on a failing bucket must not pick an open
            // certificate incident that covers it too.
            incidents={incidents
              .filter((i) => i.kind === "down")
              .map((i) => ({
                from: new Date(i.started_at).getTime(),
                to: i.resolved_at ? new Date(i.resolved_at).getTime() : null,
              }))}
            maintenance={maintenanceSpans(maintenance, monitor)}
          />
        </div>
      </section>

      {selection && (
        <Suspense fallback={<p className="text-[13px] text-ink-3">{t("common.loading")}</p>}>
          <WindowPanel
            monitorId={monitor.id}
            from={new Date(selection.from).toISOString()}
            to={new Date(Math.min(selection.to ?? Infinity, new Date(series.to).getTime())).toISOString()}
            clearHref={base}
            failuresShown={config.resultsLimit}
            incidentsShown={config.monitorIncidents}
            beforeChart={selection.from < new Date(series.from).getTime()}
            hasPods={hasPods}
            inverted={monitor.inverted}
          />
        </Suspense>
      )}

      <div className="mon-split">
        {hasPods ? (
          <>
            {/* Its own boundary: a slow cluster must not hold back the page. */}
            <Suspense fallback={<PodsLoading t={t} />}>
              <PodsPanel monitorId={monitor.id} />
            </Suspense>
            {incidentsPanel}
          </>
        ) : (
          <>
            {incidentsPanel}
            {settingsPanel(false)}
          </>
        )}
      </div>

      <section className="panel" aria-labelledby="mon-history-title">
        <div className="mon-ph">
          <h2 id="mon-history-title">{t("monitorPage.history.title")}</h2>
          <nav aria-label={t("monitorPage.history.range")} className="mon-ph-end mon-seg">
            {historyRanges.map((h) => (
              <Link
                key={h.key}
                href={`/monitors/${monitor.id}?range=${range}&history=${h.key}${selectionQuery(selection)}`}
                scroll={false}
                aria-current={h.key === history.key ? "true" : undefined}
              >
                {duration(h.hours * 3600, t)}
              </Link>
            ))}
          </nav>
        </div>
        {results.length === 0 ? (
          <p className="px-4 pt-2 pb-4 text-[13px] text-ink-3">{t("monitorPage.history.empty")}</p>
        ) : (
          <>
            <div className="p-4">
              {/* Its own boundary: the grid must not hold back the page. */}
              <Suspense fallback={<div className="h-52 rounded-md bg-well" aria-hidden />}>
                <HistoryGrid monitorId={monitor.id} hours={history.hours} inspectBase={`/monitors/${monitor.id}?history=${history.key}`} />
              </Suspense>
            </div>
            <details className="mon-more border-t border-line pt-3">
              <summary>
                <ChevronIcon width={14} height={14} />
                {t.n("monitorPage.history.recent", Math.min(results.length, config.resultsLimit))}
              </summary>
              <div className="-mx-4 mt-2 -mb-3.5">
                <CheckRows results={results.slice(0, config.resultsLimit)} inverted={monitor.inverted} t={t} />
              </div>
            </details>
          </>
        )}
      </section>

      {hasPods && settingsPanel(true)}
    </div>
  );
}

/** "1/2": the pods behind the monitor that are ready, out of all of them. */
async function PodsReadoutValue({ monitorId }: { monitorId: number }) {
  const t = await getT();
  // The panel below says why when the cluster cannot be read.
  const w = await readWorkload(monitorId).catch(() => null);
  if (!w?.available) return <span className="text-ink-3">{t("common.none")}</span>;
  return (
    <span className={w.ready < w.total ? "text-warn-ink" : undefined}>
      {w.ready}
      <small className="ml-0.5 text-[13px] leading-none font-[550] text-ink-3 [font-stretch:100%]">/ {w.total}</small>
    </span>
  );
}

export default async function Page({ params, searchParams }: PageProps<"/monitors/[id]">) {
  const t = await getT();
  return (
    <Suspense fallback={<p className="text-[13px] text-ink-3">{t("common.loading")}</p>}>
      <MonitorDetail params={params} searchParams={searchParams} />
    </Suspense>
  );
}
