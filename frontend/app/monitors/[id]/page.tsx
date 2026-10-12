import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import {
  ApiError,
  type CheckResult,
  getMonitor,
  getSeries,
  getUptime,
  type Incident,
  listIncidents,
  listResults,
  type Monitor,
  type Series,
  type UptimeWindow,
} from "@/lib/api";
import { config } from "@/lib/config";
import { duration, uptime } from "@/lib/format";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { BackendError } from "@/app/ui/backend-error";
import { HistoryStrip } from "@/app/ui/history-strip";
import { IncidentTable } from "@/app/ui/incident-table";
import { LatencyChart } from "@/app/ui/latency-chart";
import { LiveDuration } from "@/app/ui/live-duration";
import { LocalTime } from "@/app/ui/local-time";
import { SilenceForm } from "@/app/ui/maintenance-forms";
import { KindChip, monitorLabel } from "@/app/ui/monitor-name";
import { PodsPanel } from "@/app/ui/pods-panel";
import { EditMonitorForm, MonitorActions } from "@/app/ui/monitor-settings";
import { displayState, StatusBadge } from "@/app/ui/status";
import { TimeAgo } from "@/app/ui/time-ago";

async function MonitorDetail({
  params,
  searchParams,
}: {
  params: Promise<{ id: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const id = Number((await params).id);
  if (!Number.isInteger(id) || id <= 0) notFound();

  // Only the offered ranges are accepted; anything else falls back to the default.
  const requested = Number((await searchParams).range);
  const range = config.chartRanges.includes(requested) ? requested : config.chartRanges[0];

  let monitor: Monitor;
  let results: CheckResult[];
  let incidents: Incident[];
  let windows: UptimeWindow[];
  let series: Series;
  try {
    [monitor, results, incidents, windows, series] = await Promise.all([
      getMonitor(id),
      listResults(id, config.resultsLimit),
      listIncidents(config.incidentsLimit, id),
      getUptime(id, config.uptimeWindows),
      getSeries(id, range),
    ]);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return <BackendError what="Monitor durumu" />;
  }

  const label = monitorLabel(monitor);

  // Days left on the certificate as of the latest check, so the page renders
  // the same on the server and needs no clock of its own.
  const cert = monitor.last_result?.cert_expires_at
    ? {
        expiresAt: monitor.last_result.cert_expires_at,
        daysLeft: Math.floor(
          (new Date(monitor.last_result.cert_expires_at).getTime() -
            new Date(monitor.last_result.checked_at).getTime()) /
            86_400_000,
        ),
      }
    : null;

  return (
    <>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          {monitor.namespace && (
            <p className="mb-1 flex items-center gap-2 text-sm text-zinc-500">
              namespace <span className="font-medium text-foreground">{monitor.namespace}</span>
              {label.kind && <KindChip kind={label.kind} />}
            </p>
          )}
          <h1 className="text-2xl font-semibold tracking-tight">{label.name}</h1>
          <p className="mt-1 break-all text-sm text-zinc-500">
            {monitor.http?.method ?? monitor.type.toUpperCase()} · {monitor.target}
          </p>
          {monitor.http && (
            <p className="mt-1 text-xs text-zinc-500">
              Beklenen durum: {monitor.http.expected_status}
              {monitor.http.keyword && <> · aranan metin: “{monitor.http.keyword}”</>}
              {Object.keys(monitor.http.headers).length > 0 &&
                ` · ${Object.keys(monitor.http.headers).length} header`}
              {monitor.http.tls_skip_verify && " · TLS doğrulanmıyor"}
            </p>
          )}
        </div>
        <MonitorActions monitor={monitor} />
      </header>

      <dl className={`grid grid-cols-2 gap-3 ${cert ? "sm:grid-cols-4" : "sm:grid-cols-3"}`}>
        <Field label="Durum">
          <StatusBadge state={displayState(monitor)} />
        </Field>
        <Field label="Son yanıt süresi">
          {monitor.last_result?.up ? `${monitor.last_result.latency_ms} ms` : "–"}
        </Field>
        <Field label="Kontrol aralığı">
          {duration(monitor.interval_seconds)} · zaman aşımı {duration(monitor.timeout_seconds)}
          <div className="mt-0.5 text-xs font-normal text-zinc-500">
            alarm eşiği: art arda {monitor.failure_threshold} hata
          </div>
        </Field>
        {cert && (
          <Field label="TLS sertifikası">
            <span className={monitor.cert_warning ? "text-amber-700 dark:text-amber-400" : undefined}>
              {cert.daysLeft < 0 ? "Süresi doldu" : `${cert.daysLeft} gün kaldı`}
            </span>
            <div className="mt-0.5 text-xs font-normal text-zinc-500">
              bitiş: <LocalTime iso={cert.expiresAt} seconds={false} />
            </div>
          </Field>
        )}
      </dl>

      {monitor.enabled &&
        (monitor.maintenance_until ? (
          <p className="rounded-lg border border-black/10 bg-black/5 p-4 text-sm dark:border-white/15 dark:bg-white/5">
            Bakımda: <LocalTime iso={monitor.maintenance_until} seconds={false} /> tarihine kadar yeni
            olay açılmaz ve bildirim gitmez.{" "}
            <Link href="/maintenance" className="underline">
              Bakım pencereleri
            </Link>
          </p>
        ) : (
          <SilenceForm monitorId={monitor.id} durations={config.maintenanceDurations} />
        ))}

      {!monitor.enabled && (
        <p className="rounded-lg border border-black/10 bg-black/5 p-4 text-sm dark:border-white/15 dark:bg-white/5">
          Bu monitor durduruldu; kontrol yapılmıyor ve alarm üretilmiyor.
        </p>
      )}

      {monitor.cert_warning && (
        <p
          role="alert"
          className="rounded-lg border border-amber-600/40 bg-amber-500/10 p-4 text-sm text-amber-800 dark:text-amber-300"
        >
          Bu hedefin TLS sertifikasının süresi dolmak üzere
          {cert && cert.daysLeft >= 0 && `: ${cert.daysLeft} gün kaldı`}.
        </p>
      )}

      {monitor.open_incident && (
        <p
          role="alert"
          className="rounded-lg border border-red-600/40 bg-red-600/5 p-4 text-sm font-medium text-red-700 dark:text-red-400"
        >
          Olay devam ediyor: <LiveDuration since={monitor.open_incident.started_at} />
        </p>
      )}

      {monitor.enabled && monitor.last_result?.error && (
        <p
          role="alert"
          className="rounded-lg border border-red-600/40 bg-red-600/5 p-4 text-sm text-red-700 dark:text-red-400"
        >
          Son hata (<TimeAgo iso={monitor.last_result.checked_at} />):{" "}
          <span className="font-mono">{monitor.last_result.error}</span>
        </p>
      )}

      <section aria-label="Uptime">
        <dl className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          {windows.map((w) => (
            <div
              key={w.window_seconds}
              className="rounded-lg border border-black/10 p-4 dark:border-white/15"
            >
              <dt className="text-xs text-zinc-500">Uptime · son {duration(w.window_seconds)}</dt>
              <dd className="mt-1 text-2xl font-semibold">{uptime(w.uptime_percent)}</dd>
              <dd className="mt-0.5 text-xs text-zinc-500">
                {w.checks === 0
                  ? "Bu dönemde kontrol yok"
                  : `${w.checks.toLocaleString("tr-TR")} kontrol, ${(w.checks - w.up_checks).toLocaleString("tr-TR")} başarısız`}
              </dd>
            </div>
          ))}
        </dl>
      </section>

      <section>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Yanıt süresi (ms)</h2>
          <nav aria-label="Grafik aralığı" className="flex gap-1 text-sm">
            {config.chartRanges
              .toSorted((a, b) => a - b)
              .map((r) => (
                <Link
                  key={r}
                  href={`/monitors/${monitor.id}?range=${r}`}
                  scroll={false}
                  aria-current={r === range ? "true" : undefined}
                  className={`rounded-md px-3 py-1 ${
                    r === range
                      ? "bg-black/5 font-medium dark:bg-white/10"
                      : "text-zinc-500 hover:text-inherit"
                  }`}
                >
                  {duration(r)}
                </Link>
              ))}
          </nav>
        </div>
        <LatencyChart series={series} />
        <p className="mt-3 text-xs text-zinc-500">
          Her nokta {duration(series.bucket_seconds)} içindeki başarılı kontrollerin ortalamasıdır.
        </p>
      </section>

      {monitor.source && (
        <section>
          <h2 className="mb-2 text-sm font-semibold">Pod&apos;lar</h2>
          {/* Its own boundary: a slow cluster must not hold back the page. */}
          <Suspense fallback={<p className="text-sm text-zinc-500">Cluster okunuyor…</p>}>
            <PodsPanel monitorId={monitor.id} />
          </Suspense>
        </section>
      )}

      <section>
        <h2 className="mb-2 text-sm font-semibold">Son {config.historyLimit} kontrol</h2>
        <HistoryStrip results={results} slots={config.historyLimit} />
      </section>

      <details className="rounded-lg border border-black/10 dark:border-white/15">
        <summary className="cursor-pointer px-5 py-3 text-sm font-semibold">Ayarlar</summary>
        <div className="border-t border-black/10 p-5 dark:border-white/15">
          <EditMonitorForm monitor={monitor} />
        </div>
      </details>

      <section>
        <h2 className="mb-2 text-sm font-semibold">Olaylar</h2>
        {incidents.length === 0 ? (
          <p className="text-sm text-zinc-500">Bu monitor için olay kaydı yok.</p>
        ) : (
          <IncidentTable incidents={incidents} showMonitor={false} />
        )}
      </section>

      <section>
        <h2 className="mb-2 text-sm font-semibold">Kontrol geçmişi</h2>
        {results.length === 0 ? (
          <p className="text-sm text-zinc-500">Henüz kontrol yapılmadı.</p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/15">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-black/10 text-xs text-zinc-500 dark:border-white/15">
                <tr>
                  <th className="px-4 py-2 font-medium">Zaman</th>
                  <th className="px-4 py-2 font-medium">Sonuç</th>
                  <th className="px-4 py-2 text-right font-medium">Yanıt süresi</th>
                  <th className="px-4 py-2 text-right font-medium">HTTP kodu</th>
                  <th className="px-4 py-2 font-medium">Hata</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-black/5 dark:divide-white/10">
                {results.map((r) => (
                  <tr key={r.checked_at}>
                    <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                      <LocalTime iso={r.checked_at} />
                    </td>
                    <td className="px-4 py-2">
                      <StatusBadge state={r.up ? "up" : "down"} />
                    </td>
                    <td className="px-4 py-2 text-right tabular-nums">{r.latency_ms} ms</td>
                    <td className="px-4 py-2 text-right tabular-nums">{r.status_code ?? "–"}</td>
                    <td className="px-4 py-2 font-mono text-xs text-zinc-500">{r.error ?? ""}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-black/10 p-4 dark:border-white/15">
      <dt className="text-xs text-zinc-500">{label}</dt>
      <dd className="mt-1 text-sm font-medium">{children}</dd>
    </div>
  );
}

export default function Page({ params, searchParams }: PageProps<"/monitors/[id]">) {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Tüm monitor&apos;lar
      </Link>
      <LiveRefresh />
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <MonitorDetail params={params} searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
