import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import {
  ApiError,
  type CheckResult,
  getGrid,
  getMonitor,
  getSeries,
  getSeriesBetween,
  getUptime,
  type Incident,
  listIncidents,
  listResults,
  type Monitor,
  type Series,
  type UptimeWindow,
} from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { duration, milliseconds, uptime } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { typeLabel } from "@/lib/monitor-types";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { HeartbeatBar } from "@/app/ui/heartbeat-bar";
import { IncidentTable } from "@/app/ui/incident-table";
import { LatencyChart, type Span } from "@/app/ui/latency-chart";
import { LiveDuration } from "@/app/ui/live-duration";
import { LocalTime } from "@/app/ui/local-time";
import { SilenceForm } from "@/app/ui/maintenance-forms";
import { KindChip, monitorLabel } from "@/app/ui/monitor-name";
import { MonitorActions } from "@/app/ui/monitor-settings";
import { MonitorChannels } from "@/app/ui/monitor-channels";
import { PodsPanel } from "@/app/ui/pods-panel";
import { PushUrl } from "@/app/ui/push-url";
import { displayState, StatusBadge, StatusPill } from "@/app/ui/status";
import { Tag } from "@/app/ui/tag";
import { TimeAgo } from "@/app/ui/time-ago";
import { WindowPanel } from "@/app/ui/window-panel";
import { CheckGrid } from "@/app/ui/check-grid";

// How much of a timeframe's length the zoomed chart shows on each side of
// it, and the least it shows.
const ZOOM_MARGIN = 0.25;
const ZOOM_MARGIN_MIN_MS = 60_000;

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
    return <p className="text-sm text-muted">{t("monitorPage.history.gridError")}</p>;
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

/** The settings of the check, as label and value pairs. */
function checkSettings(m: Monitor, t: T): [string, React.ReactNode][] {
  const rows: [string, React.ReactNode][] = [
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
    rows.push([t("monitorPage.settings.method"), h.method], [t("monitorPage.settings.expectedStatus"), h.expected_status]);
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
        `“${h.keyword}”`,
      ]);
    }
    if (h.json_path) {
      rows.push([t("monitorPage.settings.jsonCondition"), `${h.json_path} ${h.json_operator} ${h.json_expected}`]);
    }
    const headers = Object.keys(h.headers);
    if (headers.length > 0) rows.push([t("monitorPage.settings.headers"), headers.join(", ")]);
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
    rows.push([t("monitorPage.settings.recordType"), m.dns.record_type]);
    rows.push([t("monitorPage.settings.resolver"), m.dns.resolver || t("monitorPage.settings.resolver.default")]);
    if (m.dns.expected) rows.push([t("monitorPage.settings.expected"), m.dns.expected]);
  }
  if (m.grpc) {
    rows.push([t("monitorPage.settings.service"), m.grpc.service || t("monitorPage.settings.service.whole")]);
    rows.push([
      "TLS",
      m.grpc.tls
        ? t("monitorPage.settings.tlsOn", { verified: tls(m.grpc.tls_skip_verify) })
        : t("monitorPage.settings.tlsOff"),
    ]);
  }
  if (m.database) {
    const d = m.database;
    if (d.username) rows.push([t("monitorPage.settings.user"), d.username]);
    rows.push([
      t("monitorPage.settings.password"),
      t(d.password ? "monitorPage.settings.password.set" : "monitorPage.settings.password.none"),
    ]);
    if (d.database) rows.push([t("monitorPage.settings.database"), d.database]);
    if (d.query) rows.push([t("monitorPage.settings.query"), d.query]);
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
    if (m.websocket.subprotocol) rows.push([t("monitorPage.settings.subprotocol"), m.websocket.subprotocol]);
    if (m.websocket.tls_skip_verify) rows.push([t("monitorPage.settings.tlsCert"), tls(true)]);
  }
  return rows;
}

function Stat({ label, value, hint, tone }: { label: string; value: React.ReactNode; hint?: React.ReactNode; tone?: string }) {
  return (
    <div className="card px-4 py-3">
      <dt className="text-xs font-medium text-muted">{label}</dt>
      <dd className={`mt-1 text-xl font-semibold tabular-nums ${tone ?? ""}`}>{value}</dd>
      {hint && <dd className="mt-0.5 text-xs text-muted">{hint}</dd>}
    </div>
  );
}

function Notice({ tone, children }: { tone: "down" | "warn" | "muted"; children: React.ReactNode }) {
  const tones = {
    down: "border-down/40 bg-down/8 text-down",
    warn: "border-warn/40 bg-warn/10 text-warn",
    muted: "border-line bg-sunken text-ink",
  };
  return (
    <p role={tone === "muted" ? undefined : "alert"} className={`rounded-xl border p-4 text-sm ${tones[tone]}`}>
      {children}
    </p>
  );
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
  try {
    let ranged: Series | null;
    [monitor, results, incidents, windows, ranged] = await Promise.all([
      getMonitor(id),
      listResults(id, Math.max(config.historyLimit, config.resultsLimit)),
      listIncidents(config.incidentsLimit, id),
      getUptime(id, config.uptimeWindows),
      // A timeframe has its own series, read below once its end is known.
      selection ? null : getSeries(id, range),
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

  const label = monitorLabel(monitor);
  const state = displayState(monitor);
  const last = monitor.last_result;

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
  const maintenance = t("monitorPage.maintenance").split("{until}");

  return (
    <>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="mb-1.5 flex flex-wrap items-center gap-2 text-sm text-muted">
            <Link href="/" className="hover:text-ink">
              {t("monitorPage.breadcrumb.overview")}
            </Link>
            <span aria-hidden>/</span>
            <Link href={namespaceHref(monitor.namespace)} className="hover:text-ink">
              {monitor.namespace || t(MANUAL_LABEL)}
            </Link>
            {label.kind && <KindChip kind={label.kind} />}
            <KindChip kind={t(typeLabel(monitor.type))} />
            {monitor.each_pod && (
              <span className="rounded bg-sunken px-1.5 py-0.5 text-xs" title={t("monitorPage.eachPod.title")}>
                {t("monitorPage.eachPod")}
              </span>
            )}
            {monitor.managed && (
              <span
                className="rounded bg-sunken px-1.5 py-0.5 text-xs"
                title={t("monitorPage.managed.title")}
              >
                annotation
              </span>
            )}
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-2xl font-semibold tracking-tight">{label.name}</h1>
            <StatusPill state={state} t={t} />
          </div>
          {monitor.description && <p className="mt-1.5 text-sm text-muted">{monitor.description}</p>}
          {monitor.tags.length > 0 && (
            <p className="mt-2 flex flex-wrap gap-1.5">
              {monitor.tags.map((tag) => (
                <Tag key={tag} name={tag} />
              ))}
            </p>
          )}
          {monitor.target && <p className="mt-1.5 break-all font-mono text-sm text-muted">{monitor.target}</p>}
          <Suspense>
            <MonitorChannels monitorId={monitor.id} canEdit={me.can_edit} />
          </Suspense>
        </div>
        {me.can_edit && <MonitorActions monitor={monitor} />}
      </header>

      <section aria-label={t("monitorPage.recentChecks")} className="card p-5">
        <HeartbeatBar
          results={results.map((r) => ({
            t: new Date(r.checked_at).getTime(),
            up: r.up,
            ms: r.latency_ms,
            error: r.error ?? undefined,
          }))}
          slots={config.historyLimit}
        />
        <p className="mt-3 flex flex-wrap justify-between gap-x-4 gap-y-1 text-xs text-muted">
          <span>
            {t(monitor.type === "push" ? "monitorPage.interval.push" : "monitorPage.interval.check")}:{" "}
            {duration(monitor.interval_seconds, t)}
          </span>
          <span>
            {last ? (
              <>
                {t("monitorPage.lastCheck")} <TimeAgo iso={last.checked_at} />
              </>
            ) : (
              t("monitorPage.noChecksYet")
            )}
          </span>
        </p>
        {monitor.enabled && last?.error && (
          <p className="mt-3 rounded-lg bg-down/8 px-3 py-2 font-mono text-xs text-down">{last.error}</p>
        )}
        {monitor.enabled && last?.detail && (
          <p className="mt-3 rounded-lg bg-sunken px-3 py-2 font-mono text-xs text-muted">{last.detail}</p>
        )}
      </section>

      {!monitor.enabled && <Notice tone="muted">{t("monitorPage.paused")}</Notice>}
      {monitor.open_incident && (
        <Notice tone="down">
          <span className="font-semibold">{t("monitorPage.openIncident")}</span>{" "}
          <LiveDuration since={monitor.open_incident.started_at} />
        </Notice>
      )}
      {monitor.cert_warning && (
        <Notice tone="warn">
          {t("monitorPage.certWarning")}
          {cert && cert.daysLeft >= 0 && `: ${t.n("monitorPage.certWarning.daysLeft", cert.daysLeft)}`}.
        </Notice>
      )}
      {monitor.enabled &&
        (monitor.maintenance_until ? (
          <Notice tone="muted">
            {maintenance[0]}
            <LocalTime iso={monitor.maintenance_until} seconds={false} />
            {maintenance[1]}{" "}
            <Link href="/maintenance" className="underline">
              {t("monitorPage.maintenance.windows")}
            </Link>
          </Notice>
        ) : (
          me.can_edit && <SilenceForm monitorId={monitor.id} durations={config.maintenanceDurations} />
        ))}

      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <Stat
          label={t(monitor.type === "push" ? "monitorPage.stat.reported" : "monitorPage.stat.response")}
          value={last?.up ? milliseconds(last.latency_ms, t) : "–"}
          hint={t("monitorPage.stat.lastCheck")}
        />
        <Stat
          label={t("monitorPage.stat.avgResponse")}
          value={monitor.uptime_percent ? milliseconds(monitor.avg_latency_ms, t) : "–"}
          hint={t("monitorPage.stat.last", { duration: duration(monitor.window_seconds, t) })}
        />
        {windows.map((w) => (
          <Stat
            key={w.window_seconds}
            label={t("monitorPage.stat.uptime")}
            value={uptime(w.uptime_percent, t)}
            hint={t("monitorPage.stat.last", { duration: duration(w.window_seconds, t) })}
            tone={w.uptime_percent !== null && w.uptime_percent < 99 ? "text-warn" : undefined}
          />
        ))}
        {cert && (
          <Stat
            label={t("monitorPage.stat.cert")}
            value={cert.daysLeft < 0 ? t("monitorPage.stat.certExpired") : t.n("format.days", cert.daysLeft)}
            hint={<LocalTime iso={cert.expiresAt} seconds={false} />}
            tone={cert.daysLeft < 0 ? "text-down" : monitor.cert_warning ? "text-warn" : undefined}
          />
        )}
      </dl>

      {monitor.push_token && (
        <PushUrl monitorId={monitor.id} token={monitor.push_token} internalBase={config.pushInternalUrl} />
      )}

      <section className="card p-5">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">{t("monitorPage.chart.title")}</h2>
          <nav aria-label={t("monitorPage.chart.range")} className="flex gap-1 rounded-lg bg-sunken p-0.5 text-xs">
            {config.chartRanges
              .toSorted((a, b) => a - b)
              .map((r) => (
                <Link
                  key={r}
                  // Choosing a range leaves the zoomed timeframe.
                  href={`/monitors/${monitor.id}?range=${r}&history=${history.key}`}
                  scroll={false}
                  aria-current={r === range && !selection ? "true" : undefined}
                  className={`rounded-md px-2.5 py-1 ${
                    r === range && !selection ? "bg-panel font-semibold shadow-sm" : "text-muted hover:text-ink"
                  }`}
                >
                  {duration(r, t)}
                </Link>
              ))}
          </nav>
        </div>
        {selection && (
          <p className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg bg-sunken px-3 py-2 text-xs">
            <span className="text-muted">{t("monitorPage.chart.zoomed")}</span>
            <span className="font-medium tabular-nums">
              <LocalTime iso={new Date(selection.from).toISOString()} /> –{" "}
              {selection.to === null ? t("monitorPage.chart.zoomed.now") : <LocalTime iso={new Date(selection.to).toISOString()} />}
            </span>
            <Link href={`/monitors/${monitor.id}?range=${range}&history=${history.key}`} scroll={false} className="ml-auto font-medium underline hover:text-ink">
              {t("monitorPage.chart.zoomOut", { duration: duration(range, t) })}
            </Link>
          </p>
        )}
        <LatencyChart
          series={series}
          baseHref={`/monitors/${monitor.id}?range=${range}&history=${history.key}`}
          selection={selection}
          // Only outages: a click on a failing bucket must not pick an open
          // certificate incident that covers it too.
          incidents={incidents
            .filter((i) => i.kind === "down")
            .map((i) => ({
              from: new Date(i.started_at).getTime(),
              to: i.resolved_at ? new Date(i.resolved_at).getTime() : null,
            }))}
        />
        <p className="mt-3 text-xs text-muted">
          {t("monitorPage.chart.caption", { duration: duration(series.bucket_seconds, t) })}
        </p>
      </section>

      {selection && (
        <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
          <WindowPanel
            monitorId={monitor.id}
            from={new Date(selection.from).toISOString()}
            to={new Date(Math.min(selection.to ?? Infinity, new Date(series.to).getTime())).toISOString()}
            clearHref={`/monitors/${monitor.id}?range=${range}&history=${history.key}`}
            failuresShown={config.resultsLimit}
            beforeChart={selection.from < new Date(series.from).getTime()}
            hasPods={Boolean(monitor.source) || monitor.type === "workload"}
          />
        </Suspense>
      )}

      {(monitor.source || monitor.type === "workload") && (
        <section>
          <h2 className="mb-2 text-sm font-semibold">{t("monitorPage.pods.title")}</h2>
          {/* Its own boundary: a slow cluster must not hold back the page. */}
          <Suspense fallback={<p className="text-sm text-muted">{t("common.readingCluster")}</p>}>
            <PodsPanel monitorId={monitor.id} />
          </Suspense>
        </section>
      )}

      <section>
        <h2 className="mb-2 text-sm font-semibold">{t("nav.incidents")}</h2>
        {incidents.length === 0 ? (
          <p className="card p-5 text-sm text-muted">{t("monitorPage.incidents.empty")}</p>
        ) : (
          <IncidentTable incidents={incidents} showMonitor={false} />
        )}
      </section>

      <section className="card p-5">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">{t("monitorPage.history.title")}</h2>
          <nav aria-label={t("monitorPage.history.range")} className="flex gap-1 rounded-lg bg-sunken p-0.5 text-xs">
            {historyRanges.map((h) => (
              <Link
                key={h.key}
                href={`/monitors/${monitor.id}?range=${range}&history=${h.key}${selectionQuery(selection)}`}
                scroll={false}
                aria-current={h.key === history.key ? "true" : undefined}
                className={`rounded-md px-2.5 py-1 ${
                  h.key === history.key ? "bg-panel font-semibold shadow-sm" : "text-muted hover:text-ink"
                }`}
              >
                {duration(h.hours * 3600, t)}
              </Link>
            ))}
          </nav>
        </div>
        {results.length === 0 ? (
          <p className="text-sm text-muted">{t("monitorPage.history.empty")}</p>
        ) : (
          <>
            {/* Its own boundary: the grid must not hold back the page. */}
            <Suspense fallback={<div className="h-52 rounded-lg bg-sunken/60" aria-hidden />}>
              <HistoryGrid monitorId={monitor.id} hours={history.hours} inspectBase={`/monitors/${monitor.id}?history=${history.key}`} />
            </Suspense>
            <details className="mt-4 text-sm">
              <summary className="cursor-pointer text-xs text-muted">
                {t("monitorPage.history.showTable", { count: Math.min(results.length, config.resultsLimit) })}
              </summary>
              <div className="mt-2 overflow-x-auto rounded-lg border border-line">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-line text-xs text-muted">
                    <tr>
                      <th className="px-4 py-2 font-medium">{t("monitorPage.history.time")}</th>
                      <th className="px-4 py-2 font-medium">{t("monitorPage.history.result")}</th>
                      <th className="px-4 py-2 text-right font-medium">{t("monitorPage.history.duration")}</th>
                      <th className="px-4 py-2 font-medium">{t("monitorPage.history.detail")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {results.slice(0, config.resultsLimit).map((r) => (
                      <tr key={r.checked_at}>
                        <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                          <LocalTime iso={r.checked_at} />
                        </td>
                        <td className="whitespace-nowrap px-4 py-2">
                          <StatusBadge state={r.up ? "up" : "down"} t={t} />
                        </td>
                        <td className="whitespace-nowrap px-4 py-2 text-right tabular-nums">{r.latency_ms} ms</td>
                        <td className="px-4 py-2 font-mono text-xs text-muted">
                          {[r.status_code && `HTTP ${r.status_code}`, r.error ?? r.detail].filter(Boolean).join(" · ")}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </details>
          </>
        )}
      </section>

      <section className="card p-5">
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">{t("monitorPage.settings.title")}</h2>
          {me.can_edit && (
            <Link href={`/monitors/${monitor.id}/edit`} className="text-xs text-muted hover:text-ink">
              {t("common.edit")} →
            </Link>
          )}
        </div>
        <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
          {checkSettings(monitor, t).map(([name, value]) => (
            <div key={name} className="flex justify-between gap-4 border-b border-line/60 pb-2">
              <dt className="text-muted">{name}</dt>
              <dd className="min-w-0 break-words text-right font-medium">{value}</dd>
            </div>
          ))}
        </dl>
      </section>
    </>
  );
}

export default async function Page({ params, searchParams }: PageProps<"/monitors/[id]">) {
  const t = await getT();
  return (
    <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
      <MonitorDetail params={params} searchParams={searchParams} />
    </Suspense>
  );
}
