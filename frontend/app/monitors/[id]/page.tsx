import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import {
  ApiError,
  type CheckResult,
  getMonitor,
  type Incident,
  listIncidents,
  listResults,
  type Monitor,
} from "@/lib/api";
import { config } from "@/lib/config";
import { dateTime, duration, uptime } from "@/lib/format";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { DeleteMonitorButton } from "@/app/ui/delete-monitor-button";
import { BackendError } from "@/app/ui/backend-error";
import { IncidentTable } from "@/app/ui/incident-table";
import { LiveDuration } from "@/app/ui/live-duration";
import { displayState, HistoryStrip, StatusBadge } from "@/app/ui/status";
import { TimeAgo } from "@/app/ui/time-ago";

async function MonitorDetail({ params }: { params: Promise<{ id: string }> }) {
  const id = Number((await params).id);
  if (!Number.isInteger(id) || id <= 0) notFound();

  let monitor: Monitor;
  let results: CheckResult[];
  let incidents: Incident[];
  try {
    [monitor, results, incidents] = await Promise.all([
      getMonitor(id),
      listResults(id, config.resultsLimit),
      listIncidents(config.incidentsLimit, id),
    ]);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return <BackendError what="Monitor durumu" />;
  }

  return (
    <>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold tracking-tight">{monitor.name}</h1>
          <p className="mt-1 break-all text-sm text-zinc-500">
            {monitor.type.toUpperCase()} · {monitor.target}
          </p>
        </div>
        <DeleteMonitorButton id={monitor.id} name={monitor.name} />
      </header>

      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Field label="Durum">
          <StatusBadge state={displayState(monitor)} />
        </Field>
        <Field label={`Uptime (${duration(monitor.window_seconds)})`}>
          {uptime(monitor.uptime_percent)}
        </Field>
        <Field label="Ort. yanıt">
          {monitor.checks > 0 ? `${monitor.avg_latency_ms} ms` : "–"}
        </Field>
        <Field label="Kontrol aralığı">
          {duration(monitor.interval_seconds)} · zaman aşımı {duration(monitor.timeout_seconds)}
          <div className="mt-0.5 text-xs font-normal text-zinc-500">
            alarm eşiği: art arda {monitor.failure_threshold} hata
          </div>
        </Field>
      </dl>

      {monitor.open_incident && (
        <p
          role="alert"
          className="rounded-lg border border-red-600/40 bg-red-600/5 p-4 text-sm font-medium text-red-700 dark:text-red-400"
        >
          Olay devam ediyor: <LiveDuration since={monitor.open_incident.started_at} />
        </p>
      )}

      {monitor.last_result?.error && (
        <p
          role="alert"
          className="rounded-lg border border-red-600/40 bg-red-600/5 p-4 text-sm text-red-700 dark:text-red-400"
        >
          Son hata (<TimeAgo iso={monitor.last_result.checked_at} />):{" "}
          <span className="font-mono">{monitor.last_result.error}</span>
        </p>
      )}

      <section>
        <h2 className="mb-2 text-sm font-semibold">Son {config.historyLimit} kontrol</h2>
        <HistoryStrip results={results} slots={config.historyLimit} />
      </section>

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
                      {dateTime(r.checked_at)}
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

export default function Page({ params }: PageProps<"/monitors/[id]">) {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Tüm monitor&apos;lar
      </Link>
      <LiveRefresh />
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <MonitorDetail params={params} />
      </Suspense>
    </main>
  );
}
