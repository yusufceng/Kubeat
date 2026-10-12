import Link from "next/link";
import type { Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { duration, uptime } from "@/lib/format";
import { MANUAL_LABEL } from "@/lib/namespaces";
import { HistoryStrip } from "@/app/ui/history-strip";
import { LiveDuration } from "@/app/ui/live-duration";
import { KindChip, monitorLabel } from "@/app/ui/monitor-name";
import { type DisplayState, displayState, StatusBadge } from "@/app/ui/status";
import { TimeAgo } from "@/app/ui/time-ago";

/** How many monitors are in each state the UI distinguishes. */
export type StateCounts = { up: number; down: number; verifying: number; idle: number; total: number };

export function countStates(monitors: Monitor[]): StateCounts {
  const counts: StateCounts = { up: 0, down: 0, verifying: 0, idle: 0, total: monitors.length };
  for (const m of monitors) {
    const state = displayState(m);
    if (state === "up") counts.up++;
    else if (state === "down" || state === "recovering") counts.down++;
    else if (state === "verifying") counts.verifying++;
    else counts.idle++; // not checked yet, or paused
  }
  return counts;
}

/** A monitor whose latest check failed or that has an open outage. */
export function isProblem(m: Monitor): boolean {
  const state: DisplayState = displayState(m);
  return state === "down" || state === "recovering" || state === "verifying";
}

export function StateStats({ counts }: { counts: StateCounts }) {
  return (
    <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <Stat label="Çalışıyor" value={counts.up} className="text-emerald-600 dark:text-emerald-400" />
      <Stat label="Erişilemiyor" value={counts.down} className="text-red-600 dark:text-red-400" />
      <Stat
        label="Doğrulanıyor"
        value={counts.verifying}
        className="text-amber-600 dark:text-amber-400"
      />
      <Stat label="Bekleniyor / durduruldu" value={counts.idle} className="text-zinc-500" />
    </dl>
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

/**
 * Monitors with their state, recent history and statistics. The monitors
 * must have been listed with their history.
 */
export function MonitorTable({
  monitors,
  showNamespace = false,
}: {
  monitors: Monitor[];
  /** For lists that mix namespaces. */
  showNamespace?: boolean;
}) {
  const window = monitors.length > 0 ? duration(monitors[0].window_seconds) : "";

  return (
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
          {monitors.map((m) => {
            const label = monitorLabel(m);
            return (
              <tr key={m.id}>
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2">
                    {label.kind && <KindChip kind={label.kind} />}
                    <Link href={`/monitors/${m.id}`} className="truncate font-medium hover:underline">
                      {label.name}
                    </Link>
                  </div>
                  <div className="truncate text-xs text-zinc-500" title={m.target}>
                    {showNamespace && (
                      <span className="text-foreground/70">{m.namespace || MANUAL_LABEL} · </span>
                    )}
                    {m.target}
                  </div>
                </td>
                <td className="whitespace-nowrap px-4 py-3">
                  <StatusBadge state={displayState(m)} />
                  {m.open_incident && (
                    <div className="mt-0.5 text-xs text-zinc-500">
                      <LiveDuration since={m.open_incident.started_at} />
                    </div>
                  )}
                  {m.maintenance_until && (
                    <div className="mt-0.5 text-xs text-zinc-500">Bakımda, alarm yok</div>
                  )}
                  {m.cert_warning && (
                    <div className="mt-0.5 text-xs text-amber-700 dark:text-amber-400">
                      Sertifika süresi doluyor
                    </div>
                  )}
                </td>
                <td className="px-4 py-3">
                  <HistoryStrip results={m.history} slots={config.historyLimit} />
                </td>
                <td className="px-4 py-3 text-right tabular-nums">{uptime(m.uptime_percent)}</td>
                <td className="px-4 py-3 text-right tabular-nums">
                  {/* The average covers successful checks; without any there is none. */}
                  {m.uptime_percent ? `${m.avg_latency_ms} ms` : "–"}
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
  );
}

/** "3/4 çalışıyor · 1 erişilemiyor", the one-line state of a group. */
export function GroupSummary({ counts }: { counts: StateCounts }) {
  return (
    <p className="text-xs text-zinc-500">
      {counts.up}/{counts.total} çalışıyor
      {counts.down > 0 && (
        <span className="font-medium text-red-600 dark:text-red-400">
          {" "}
          · {counts.down} erişilemiyor
        </span>
      )}
      {counts.verifying > 0 && (
        <span className="text-amber-700 dark:text-amber-400"> · {counts.verifying} doğrulanıyor</span>
      )}
    </p>
  );
}
