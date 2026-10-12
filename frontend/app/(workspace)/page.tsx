import Link from "next/link";
import { Suspense } from "react";
import { type Incident, listIncidents, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { duration, milliseconds, span, uptime } from "@/lib/format";
import { compareNamespaces, MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { countStates, type StateCounts, workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { PlusIcon } from "@/app/ui/icons";
import { LiveDuration } from "@/app/ui/live-duration";
import { LocalTime } from "@/app/ui/local-time";
import { monitorLabel } from "@/app/ui/monitor-name";
import { type DisplayState, StatusBadge } from "@/app/ui/status";

/** One line of the event list: something that started or ended. */
type Event = {
  key: string;
  at: string;
  monitorId: number;
  monitorName: string;
  state: DisplayState;
  label: string;
  message: string;
};

/** The beginnings and endings of the incidents, newest first. */
function events(incidents: Incident[], limit: number): Event[] {
  const out: Event[] = [];
  for (const i of incidents) {
    const cert = i.kind === "cert";
    out.push({
      key: `${i.id}-start`,
      at: i.started_at,
      monitorId: i.monitor_id,
      monitorName: i.monitor_name,
      state: cert ? "verifying" : "down",
      label: cert ? "Sertifika uyarısı" : "Erişilemiyor",
      message: i.cause,
    });
    if (i.resolved_at) {
      const length = span(new Date(i.resolved_at).getTime() - new Date(i.started_at).getTime());
      out.push({
        key: `${i.id}-end`,
        at: i.resolved_at,
        monitorId: i.monitor_id,
        monitorName: i.monitor_name,
        state: "up",
        label: cert ? "Sertifika yenilendi" : "Düzeldi",
        message: cert ? "" : `Kesinti ${length} sürdü`,
      });
    }
  }
  return out.sort((a, b) => b.at.localeCompare(a.at)).slice(0, limit);
}

/** Uptime and response time over every monitor, weighted by their checks. */
function fleetStats(monitors: Monitor[]) {
  let checks = 0;
  let up = 0;
  let latency = 0;
  for (const m of monitors) {
    if (m.uptime_percent === null) continue;
    const upChecks = (m.checks * m.uptime_percent) / 100;
    checks += m.checks;
    up += upChecks;
    latency += m.avg_latency_ms * upChecks;
  }
  return {
    uptime: checks > 0 ? (100 * up) / checks : null,
    avgLatency: up > 0 ? Math.round(latency / up) : null,
    window: monitors[0]?.window_seconds ?? 0,
  };
}

/** Certificates by how soon they expire, as of each monitor's latest check. */
function certificates(monitors: Monitor[], limit: number) {
  return monitors
    .flatMap((m) => {
      const last = m.last_result;
      if (!m.enabled || !last?.cert_expires_at) return [];
      const days = Math.floor(
        (new Date(last.cert_expires_at).getTime() - new Date(last.checked_at).getTime()) / 86_400_000,
      );
      return [{ monitor: m, expiresAt: last.cert_expires_at, days }];
    })
    .sort((a, b) => a.days - b.days)
    .slice(0, limit);
}

function byNamespace(monitors: Monitor[]): [string, Monitor[]][] {
  const groups = new Map<string, Monitor[]>();
  for (const m of monitors) groups.set(m.namespace, [...(groups.get(m.namespace) ?? []), m]);
  return [...groups.entries()].sort(([a], [b]) => compareNamespaces(a, b));
}

/** The share of each state as one bar. */
function Distribution({ counts }: { counts: StateCounts }) {
  const parts: [number, string][] = [
    [counts.down + counts.recovering, "bg-down"],
    [counts.verifying, "bg-warn"],
    [counts.maintenance, "bg-maint"],
    [counts.up, "bg-up"],
    [counts.paused + counts.pending, "bg-idle"],
  ];
  return (
    <span className="flex h-2 w-full overflow-hidden rounded-full bg-sunken" aria-hidden>
      {parts.map(
        ([n, color]) =>
          n > 0 && <span key={color} className={color} style={{ width: `${(100 * n) / counts.total}%` }} />,
      )}
    </span>
  );
}

function Tile({
  label,
  value,
  tone,
  hint,
}: {
  label: string;
  value: React.ReactNode;
  /** A text color class; the value is muted when it is zero. */
  tone?: string;
  hint?: string;
}) {
  return (
    <div className="card p-4">
      <dt className="text-xs font-medium text-muted">{label}</dt>
      <dd className={`mt-1 whitespace-nowrap text-2xl font-semibold tabular-nums ${tone ?? ""}`}>{value}</dd>
      {hint && <dd className="mt-0.5 text-xs text-muted">{hint}</dd>}
    </div>
  );
}

async function Overview() {
  let monitors: Monitor[];
  let incidents: Incident[];
  try {
    [monitors, incidents] = await Promise.all([workspaceMonitors(), listIncidents(config.incidentsLimit)]);
  } catch (err) {
    return <BackendError what="Genel bakış" err={err} />;
  }

  if (monitors.length === 0) {
    return (
      <div className="card flex flex-col items-center gap-4 p-10 text-center">
        <h2 className="text-lg font-semibold">Henüz izlenen bir şey yok</h2>
        <p className="max-w-md text-sm text-muted">
          Cluster&apos;daki Service, Ingress ve Route&apos;ları keşfederek başlayın ya da bir adresi elle
          ekleyin: HTTP, TCP, ping, DNS, gRPC, WebSocket ya da kendi rapor veren bir iş.
        </p>
        <div className="flex flex-wrap justify-center gap-2">
          <Link href="/discovery" className="btn btn-primary">
            Cluster&apos;dan keşfet
          </Link>
          <Link href="/monitors/new" className="btn">
            <PlusIcon /> Elle ekle
          </Link>
        </div>
      </div>
    );
  }

  const counts = countStates(monitors);
  const failing = counts.down + counts.recovering;
  const fleet = fleetStats(monitors);
  const open = incidents.filter((i) => !i.resolved_at && i.kind === "down");
  const recent = events(incidents, config.overviewEvents);
  const certs = certificates(monitors, config.overviewCertificates);
  const namespaces = byNamespace(monitors);
  const zero = (n: number, tone: string) => (n > 0 ? tone : "text-muted");

  return (
    <>
      <div
        role="status"
        className={`card flex flex-wrap items-center gap-x-4 gap-y-1 border-l-4 p-4 ${
          failing > 0 ? "border-l-down" : counts.verifying > 0 ? "border-l-warn" : "border-l-up"
        }`}
      >
        <p className="text-lg font-semibold">
          {failing > 0
            ? `${failing} monitor erişilemiyor`
            : counts.verifying > 0
              ? `${counts.verifying} monitor doğrulanıyor`
              : "Tüm sistemler çalışıyor"}
        </p>
        <p className="text-sm text-muted">
          {counts.total} monitor · {namespaces.length} grup
          {fleet.uptime !== null && ` · son ${duration(fleet.window)} uptime ${uptime(fleet.uptime)}`}
        </p>
      </div>

      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <Tile label="Çalışıyor" value={counts.up} tone={zero(counts.up, "text-up")} />
        <Tile label="Erişilemiyor" value={failing} tone={zero(failing, "text-down")} />
        <Tile label="Doğrulanıyor" value={counts.verifying} tone={zero(counts.verifying, "text-warn")} />
        <Tile label="Bakımda" value={counts.maintenance} tone={zero(counts.maintenance, "text-maint")} />
        <Tile
          label="Pasif"
          value={counts.paused + counts.pending}
          tone="text-muted"
          hint={counts.pending > 0 ? `${counts.pending} ilk kontrolü bekliyor` : undefined}
        />
        <Tile
          label="Ort. yanıt"
          value={fleet.avgLatency === null ? "–" : milliseconds(fleet.avgLatency)}
          hint={`son ${duration(fleet.window)}`}
        />
      </dl>

      {open.length > 0 && (
        <section aria-label="Devam eden kesintiler" className="card overflow-hidden border-down/40">
          <h2 className="border-b border-line bg-down/8 px-4 py-2.5 text-sm font-semibold text-down">
            Devam eden kesintiler ({open.length})
          </h2>
          <ul className="divide-y divide-line">
            {open.map((i) => (
              <li key={i.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-4 py-2.5 text-sm">
                <Link href={`/monitors/${i.monitor_id}`} className="font-medium hover:underline">
                  {i.monitor_name}
                </Link>
                <span className="font-mono text-xs text-muted">{i.cause}</span>
                <span className="ml-auto whitespace-nowrap tabular-nums text-down">
                  <LiveDuration since={i.started_at} />
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <div className="grid gap-5 xl:grid-cols-5">
        <section aria-label="Son olaylar" className="card overflow-hidden xl:col-span-3">
          <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
            <h2 className="text-sm font-semibold">Son olaylar</h2>
            <Link href="/incidents" className="text-xs text-muted hover:text-ink">
              Tümü →
            </Link>
          </div>
          {recent.length === 0 ? (
            <p className="p-6 text-center text-sm text-muted">
              Kayıtlı olay yok. Bir monitor alarm eşiği kadar art arda başarısız olursa burada görünür.
            </p>
          ) : (
            <ul className="divide-y divide-line">
              {recent.map((e) => (
                <li key={e.key} className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 px-4 py-2.5 text-sm">
                  <span className="row-span-2 pt-0.5">
                    <StatusBadge state={e.state} />
                  </span>
                  <span className="flex flex-wrap items-baseline justify-between gap-x-3">
                    <Link href={`/monitors/${e.monitorId}`} className="font-medium hover:underline">
                      {e.monitorName}
                    </Link>
                    <span className="whitespace-nowrap text-xs tabular-nums text-muted">
                      <LocalTime iso={e.at} />
                    </span>
                  </span>
                  <span className="truncate text-xs text-muted" title={e.message}>
                    {e.label}
                    {e.message && ` · ${e.message}`}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>

        <div className="flex flex-col gap-5 xl:col-span-2">
          <section aria-label="Gruplar" className="card overflow-hidden">
            <h2 className="border-b border-line px-4 py-2.5 text-sm font-semibold">Namespace&apos;ler</h2>
            <ul className="divide-y divide-line">
              {namespaces.map(([namespace, group]) => {
                const c = countStates(group);
                const bad = c.down + c.recovering;
                return (
                  <li key={namespace}>
                    <Link href={namespaceHref(namespace)} className="block px-4 py-2.5 hover:bg-sunken/60">
                      <span className="mb-1.5 flex items-baseline justify-between gap-3 text-sm">
                        <span className="truncate font-medium">{namespace || MANUAL_LABEL}</span>
                        <span className="whitespace-nowrap text-xs tabular-nums text-muted">
                          {bad > 0 && <span className="font-semibold text-down">{bad} sorunlu · </span>}
                          {c.up}/{c.total}
                        </span>
                      </span>
                      <Distribution counts={c} />
                    </Link>
                  </li>
                );
              })}
            </ul>
          </section>

          {certs.length > 0 && (
            <section aria-label="Sertifikalar" className="card overflow-hidden">
              <h2 className="border-b border-line px-4 py-2.5 text-sm font-semibold">
                Süresi en yakın sertifikalar
              </h2>
              <ul className="divide-y divide-line">
                {certs.map(({ monitor, expiresAt, days }) => (
                  <li key={monitor.id} className="flex items-baseline justify-between gap-3 px-4 py-2.5 text-sm">
                    <Link href={`/monitors/${monitor.id}`} className="truncate font-medium hover:underline">
                      {monitorLabel(monitor).name}
                    </Link>
                    <span
                      className={`whitespace-nowrap text-xs tabular-nums ${
                        days < 0 ? "font-semibold text-down" : monitor.cert_warning ? "font-semibold text-warn" : "text-muted"
                      }`}
                      title={expiresAt}
                    >
                      {days < 0 ? "süresi doldu" : `${days} gün`}
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      </div>
    </>
  );
}

export default function Home() {
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">Genel bakış</h1>
        <Link href="/monitors/new" className="btn lg:hidden">
          <PlusIcon /> Yeni monitor
        </Link>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <Overview />
      </Suspense>
    </>
  );
}
