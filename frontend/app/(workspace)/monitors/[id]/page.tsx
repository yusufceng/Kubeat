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
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { duration, milliseconds, uptime } from "@/lib/format";
import { typeLabel } from "@/lib/monitor-types";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { HeartbeatBar } from "@/app/ui/heartbeat-bar";
import { IncidentTable } from "@/app/ui/incident-table";
import { LatencyChart } from "@/app/ui/latency-chart";
import { LiveDuration } from "@/app/ui/live-duration";
import { LocalTime } from "@/app/ui/local-time";
import { SilenceForm } from "@/app/ui/maintenance-forms";
import { KindChip, monitorLabel } from "@/app/ui/monitor-name";
import { MonitorActions } from "@/app/ui/monitor-settings";
import { PodsPanel } from "@/app/ui/pods-panel";
import { PushUrl } from "@/app/ui/push-url";
import { displayState, StatusBadge, StatusPill } from "@/app/ui/status";
import { Tag } from "@/app/ui/tag";
import { TimeAgo } from "@/app/ui/time-ago";

/** The settings of the check, as label and value pairs. */
function checkSettings(m: Monitor): [string, React.ReactNode][] {
  const rows: [string, React.ReactNode][] = [
    ["Tip", typeLabel(m.type)],
    [m.type === "push" ? "Rapor aralığı" : "Kontrol aralığı", duration(m.interval_seconds)],
    [m.type === "push" ? "Gecikme payı" : "Zaman aşımı", duration(m.timeout_seconds)],
    ["Alarm eşiği", `art arda ${m.failure_threshold} hata`],
  ];
  if (m.retry_interval_seconds > 0) rows.push(["Tekrar aralığı", duration(m.retry_interval_seconds)]);
  if (m.inverted) rows.push(["Ters mod", "hedef başarısızken çalışıyor sayılır"]);

  const tls = (skip: boolean) => (skip ? "doğrulanmıyor" : "doğrulanıyor");
  if (m.http) {
    const h = m.http;
    rows.push(["Metot", h.method], ["Kabul edilen kodlar", h.expected_status]);
    rows.push(["Yönlendirme", h.max_redirects === 0 ? "izlenmez" : `en çok ${h.max_redirects}`]);
    if (h.auth) rows.push(["Kimlik doğrulama", h.auth === "basic" ? `Basic (${h.auth_user})` : "Bearer token"]);
    if (h.keyword) rows.push([h.invert_keyword ? "Bulunmaması gereken metin" : "Aranan metin", `“${h.keyword}”`]);
    if (h.json_path) rows.push(["JSON koşulu", `${h.json_path} ${h.json_operator} ${h.json_expected}`]);
    const headers = Object.keys(h.headers);
    if (headers.length > 0) rows.push(["Header'lar", headers.join(", ")]);
    if (h.body) rows.push(["Gövde", `${h.body.length} karakter, ${h.content_type || "tipi belirtilmemiş"}`]);
    if (h.tls_skip_verify) rows.push(["TLS sertifikası", tls(true)]);
  }
  if (m.tcp?.tls) rows.push(["TLS", `el sıkışması yapılır, sertifika ${tls(m.tcp.tls_skip_verify)}`]);
  if (m.dns) {
    rows.push(["Kayıt tipi", m.dns.record_type]);
    rows.push(["Çözümleyici", m.dns.resolver || "ortamın DNS'i"]);
    if (m.dns.expected) rows.push(["Beklenen değer", m.dns.expected]);
  }
  if (m.grpc) {
    rows.push(["Servis", m.grpc.service || "sunucunun geneli"]);
    rows.push(["TLS", m.grpc.tls ? `açık, sertifika ${tls(m.grpc.tls_skip_verify)}` : "kapalı"]);
  }
  if (m.database) {
    const d = m.database;
    if (d.username) rows.push(["Kullanıcı", d.username]);
    rows.push(["Parola", d.password ? "kayıtlı" : "yok"]);
    if (d.database) rows.push(["Veritabanı", d.database]);
    if (d.query) rows.push(["Sorgu", d.query]);
    rows.push(["TLS", d.tls ? `açık, sertifika ${tls(d.tls_skip_verify)}` : "kapalı"]);
  }
  if (m.workload) {
    rows.push(["Hazır olması gereken", m.workload.min_ready > 0 ? `en az ${m.workload.min_ready} pod` : "tüm pod'lar"]);
  }
  if (m.websocket) {
    if (m.websocket.subprotocol) rows.push(["Alt protokol", m.websocket.subprotocol]);
    if (m.websocket.tls_skip_verify) rows.push(["TLS sertifikası", tls(true)]);
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

  // Only the offered ranges are accepted; anything else falls back to the default.
  const requested = Number((await searchParams).range);
  const range = config.chartRanges.includes(requested) ? requested : config.chartRanges[0];

  const me = await currentUser();
  let monitor: Monitor;
  let results: CheckResult[];
  let incidents: Incident[];
  let windows: UptimeWindow[];
  let series: Series;
  try {
    [monitor, results, incidents, windows, series] = await Promise.all([
      getMonitor(id),
      listResults(id, Math.max(config.historyLimit, config.resultsLimit)),
      listIncidents(config.incidentsLimit, id),
      getUptime(id, config.uptimeWindows),
      getSeries(id, range),
    ]);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return <BackendError what="Monitor durumu" err={err} />;
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

  return (
    <>
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <p className="mb-1.5 flex flex-wrap items-center gap-2 text-sm text-muted">
            <Link href="/" className="hover:text-ink">
              Genel bakış
            </Link>
            <span aria-hidden>/</span>
            <Link href={namespaceHref(monitor.namespace)} className="hover:text-ink">
              {monitor.namespace || MANUAL_LABEL}
            </Link>
            {label.kind && <KindChip kind={label.kind} />}
            <KindChip kind={typeLabel(monitor.type)} />
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-2xl font-semibold tracking-tight">{label.name}</h1>
            <StatusPill state={state} />
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
        </div>
        {me.can_edit && <MonitorActions monitor={monitor} />}
      </header>

      <section aria-label="Son kontroller" className="card p-5">
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
            {monitor.type === "push" ? "Rapor beklenen aralık" : "Kontrol aralığı"}:{" "}
            {duration(monitor.interval_seconds)}
          </span>
          <span>
            {last ? (
              <>
                Son kontrol <TimeAgo iso={last.checked_at} />
              </>
            ) : (
              "Henüz kontrol yapılmadı"
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

      {!monitor.enabled && (
        <Notice tone="muted">Bu monitor durduruldu; kontrol yapılmıyor ve alarm üretilmiyor.</Notice>
      )}
      {monitor.open_incident && (
        <Notice tone="down">
          <span className="font-semibold">Kesinti devam ediyor:</span>{" "}
          <LiveDuration since={monitor.open_incident.started_at} />
        </Notice>
      )}
      {monitor.cert_warning && (
        <Notice tone="warn">
          Bu hedefin TLS sertifikasının süresi dolmak üzere
          {cert && cert.daysLeft >= 0 && `: ${cert.daysLeft} gün kaldı`}.
        </Notice>
      )}
      {monitor.enabled &&
        (monitor.maintenance_until ? (
          <Notice tone="muted">
            Bakımda: <LocalTime iso={monitor.maintenance_until} seconds={false} /> tarihine kadar yeni olay
            açılmaz ve bildirim gitmez.{" "}
            <Link href="/maintenance" className="underline">
              Bakım pencereleri
            </Link>
          </Notice>
        ) : (
          me.can_edit && <SilenceForm monitorId={monitor.id} durations={config.maintenanceDurations} />
        ))}

      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <Stat
          label={monitor.type === "push" ? "Bildirilen süre" : "Yanıt"}
          value={last?.up ? milliseconds(last.latency_ms) : "–"}
          hint="son kontrol"
        />
        <Stat
          label="Ort. yanıt"
          value={monitor.uptime_percent ? milliseconds(monitor.avg_latency_ms) : "–"}
          hint={`son ${duration(monitor.window_seconds)}`}
        />
        {windows.map((w) => (
          <Stat
            key={w.window_seconds}
            label="Uptime"
            value={uptime(w.uptime_percent)}
            hint={`son ${duration(w.window_seconds)}`}
            tone={w.uptime_percent !== null && w.uptime_percent < 99 ? "text-warn" : undefined}
          />
        ))}
        {cert && (
          <Stat
            label="Sertifika"
            value={cert.daysLeft < 0 ? "Doldu" : `${cert.daysLeft} gün`}
            hint={<LocalTime iso={cert.expiresAt} seconds={false} />}
            tone={cert.daysLeft < 0 ? "text-down" : monitor.cert_warning ? "text-warn" : undefined}
          />
        )}
      </dl>

      {monitor.push_token && <PushUrl token={monitor.push_token} internalBase={config.pushInternalUrl} />}

      <section className="card p-5">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Yanıt süresi (ms)</h2>
          <nav aria-label="Grafik aralığı" className="flex gap-1 rounded-lg bg-sunken p-0.5 text-xs">
            {config.chartRanges
              .toSorted((a, b) => a - b)
              .map((r) => (
                <Link
                  key={r}
                  href={`/monitors/${monitor.id}?range=${r}`}
                  scroll={false}
                  aria-current={r === range ? "true" : undefined}
                  className={`rounded-md px-2.5 py-1 ${
                    r === range ? "bg-panel font-semibold shadow-sm" : "text-muted hover:text-ink"
                  }`}
                >
                  {duration(r)}
                </Link>
              ))}
          </nav>
        </div>
        <LatencyChart series={series} />
        <p className="mt-3 text-xs text-muted">
          Her nokta {duration(series.bucket_seconds)} içindeki başarılı kontrollerin ortalamasıdır.
        </p>
      </section>

      {(monitor.source || monitor.type === "workload") && (
        <section>
          <h2 className="mb-2 text-sm font-semibold">Pod&apos;lar</h2>
          {/* Its own boundary: a slow cluster must not hold back the page. */}
          <Suspense fallback={<p className="text-sm text-muted">Cluster okunuyor…</p>}>
            <PodsPanel monitorId={monitor.id} />
          </Suspense>
        </section>
      )}

      <section>
        <h2 className="mb-2 text-sm font-semibold">Olaylar</h2>
        {incidents.length === 0 ? (
          <p className="card p-5 text-sm text-muted">Bu monitor için olay kaydı yok.</p>
        ) : (
          <IncidentTable incidents={incidents} showMonitor={false} />
        )}
      </section>

      <section>
        <h2 className="mb-2 text-sm font-semibold">Kontrol geçmişi</h2>
        {results.length === 0 ? (
          <p className="card p-5 text-sm text-muted">Henüz kontrol yapılmadı.</p>
        ) : (
          <div className="card overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line text-xs text-muted">
                <tr>
                  <th className="px-4 py-2 font-medium">Zaman</th>
                  <th className="px-4 py-2 font-medium">Sonuç</th>
                  <th className="px-4 py-2 text-right font-medium">Süre</th>
                  <th className="px-4 py-2 font-medium">Ayrıntı</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {results.slice(0, config.resultsLimit).map((r) => (
                  <tr key={r.checked_at}>
                    <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                      <LocalTime iso={r.checked_at} />
                    </td>
                    <td className="whitespace-nowrap px-4 py-2">
                      <StatusBadge state={r.up ? "up" : "down"} />
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
        )}
      </section>

      <section className="card p-5">
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">Kontrol ayarları</h2>
          {me.can_edit && (
            <Link href={`/monitors/${monitor.id}/edit`} className="text-xs text-muted hover:text-ink">
              Düzenle →
            </Link>
          )}
        </div>
        <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2">
          {checkSettings(monitor).map(([name, value]) => (
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

export default function Page({ params, searchParams }: PageProps<"/monitors/[id]">) {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
      <MonitorDetail params={params} searchParams={searchParams} />
    </Suspense>
  );
}
