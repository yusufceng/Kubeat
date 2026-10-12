import Link from "next/link";
import { Suspense } from "react";
import { type CheckResult, listMonitors, listResults, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { duration, uptime } from "@/lib/format";
import { AddMonitorForm } from "@/app/ui/add-monitor-form";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { BackendError } from "@/app/ui/backend-error";
import { HistoryStrip } from "@/app/ui/history-strip";
import { LiveDuration } from "@/app/ui/live-duration";
import { KindChip, monitorLabel } from "@/app/ui/monitor-name";
import { type DisplayState, displayState, StatusBadge } from "@/app/ui/status";
import { TimeAgo } from "@/app/ui/time-ago";

async function Monitors() {
  let monitors: Monitor[];
  let histories: CheckResult[][];
  try {
    monitors = await listMonitors();
    histories = await Promise.all(monitors.map((m) => listResults(m.id, config.historyLimit)));
  } catch {
    return <BackendError what="Monitor durumları" />;
  }

  if (monitors.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-black/15 p-8 text-center text-sm text-zinc-500 dark:border-white/20">
        Henüz monitor yok. <Link href="/discovery" className="underline">Keşif</Link> ile
        cluster&apos;daki servisleri bulun ya da aşağıdaki formdan elle ekleyin.
      </p>
    );
  }

  const states = monitors.map(displayState);
  const count = (...wanted: DisplayState[]) => states.filter((s) => wanted.includes(s)).length;
  const window = duration(monitors[0].window_seconds);

  // Namespaces alphabetically, monitors entered by hand last.
  const groups = new Map<string, number[]>();
  monitors.forEach((m, i) => groups.set(m.namespace, [...(groups.get(m.namespace) ?? []), i]));
  const namespaces = [...groups.keys()].sort((a, b) =>
    a === "" ? 1 : b === "" ? -1 : a.localeCompare(b),
  );

  return (
    <>
      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Stat label="Çalışıyor" value={count("up")} className="text-emerald-600 dark:text-emerald-400" />
        <Stat
          label="Erişilemiyor"
          value={count("down", "recovering")}
          className="text-red-600 dark:text-red-400"
        />
        <Stat
          label="Doğrulanıyor"
          value={count("verifying")}
          className="text-amber-600 dark:text-amber-400"
        />
        <Stat
          label="Bekleniyor / durduruldu"
          value={count("pending", "paused")}
          className="text-zinc-500"
        />
      </dl>

      {namespaces.map((ns) => {
        const rows = groups.get(ns)!;
        const up = rows.filter((i) => states[i] === "up").length;
        const down = rows.filter((i) => states[i] === "down" || states[i] === "recovering").length;
        const paused = rows.filter((i) => states[i] === "paused").length;
        return (
          <section key={ns} aria-label={ns || "Elle eklenenler"}>
            <div className="mb-2 flex items-baseline justify-between gap-3">
              <h2 className="text-sm font-semibold">
                {ns ? (
                  <>
                    <span className="font-normal text-zinc-500">namespace</span> {ns}
                  </>
                ) : (
                  "Elle eklenenler"
                )}
              </h2>
              <p className="text-xs text-zinc-500">
                {up}/{rows.length - paused} çalışıyor
                {paused > 0 && ` · ${paused} durduruldu`}
                {down > 0 && (
                  <span className="font-medium text-red-600 dark:text-red-400">
                    {" "}
                    · {down} erişilemiyor
                  </span>
                )}
              </p>
            </div>
            <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/15">
              <table className="w-full table-fixed text-left text-sm">
                <colgroup>
                  <col />
                  <col className="w-36" />
                  <col className="w-80" />
                  <col className="w-28" />
                  <col className="w-24" />
                  <col className="w-28" />
                </colgroup>
                <thead className="border-b border-black/10 text-xs text-zinc-500 dark:border-white/15">
                  <tr>
                    <th className="px-4 py-3 font-medium">Monitor</th>
                    <th className="px-4 py-3 font-medium">Durum</th>
                    <th className="px-4 py-3 font-medium">Son kontroller</th>
                    <th className="px-4 py-3 text-right font-medium">Uptime ({window})</th>
                    <th className="px-4 py-3 text-right font-medium">Ort. yanıt</th>
                    <th className="px-4 py-3 text-right font-medium">Son kontrol</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-black/5 dark:divide-white/10">
                  {rows.map((i) => {
                    const m = monitors[i];
                    const label = monitorLabel(m);
                    return (
                      <tr key={m.id}>
                        <td className="px-4 py-3">
                          <div className="flex items-center gap-2">
                            {label.kind && <KindChip kind={label.kind} />}
                            <Link
                              href={`/monitors/${m.id}`}
                              className="truncate font-medium hover:underline"
                            >
                              {label.name}
                            </Link>
                          </div>
                          <div className="truncate text-xs text-zinc-500" title={m.target}>
                            {m.target}
                          </div>
                        </td>
                        <td className="whitespace-nowrap px-4 py-3">
                          <StatusBadge state={states[i]} />
                          {m.open_incident && (
                            <div className="mt-0.5 text-xs text-zinc-500">
                              <LiveDuration since={m.open_incident.started_at} />
                            </div>
                          )}
                          {m.cert_warning && (
                            <div className="mt-0.5 text-xs text-amber-700 dark:text-amber-400">
                              Sertifika süresi doluyor
                            </div>
                          )}
                        </td>
                        <td className="px-4 py-3">
                          <HistoryStrip results={histories[i]} slots={config.historyLimit} />
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {uptime(m.uptime_percent)}
                        </td>
                        <td className="px-4 py-3 text-right tabular-nums">
                          {m.checks > 0 ? `${m.avg_latency_ms} ms` : "–"}
                        </td>
                        <td className="whitespace-nowrap px-4 py-3 text-right text-zinc-500">
                          {m.last_result ? <TimeAgo iso={m.last_result.checked_at} /> : "–"}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </section>
        );
      })}
    </>
  );
}

function Stat({ label, value, className }: { label: string; value: number; className: string }) {
  return (
    <div className="rounded-lg border border-black/10 p-4 dark:border-white/15">
      <dt className="text-xs text-zinc-500">{label}</dt>
      <dd className={`mt-1 text-3xl font-semibold tabular-nums ${className}`}>{value}</dd>
    </div>
  );
}

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <h1 className="text-2xl font-semibold tracking-tight">Monitor&apos;lar</h1>
      <LiveRefresh />
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <Monitors />
      </Suspense>
      <section className="rounded-lg border border-black/10 p-5 dark:border-white/15">
        <h2 className="mb-4 text-sm font-semibold">Elle monitor ekle</h2>
        <AddMonitorForm />
      </section>
    </main>
  );
}
