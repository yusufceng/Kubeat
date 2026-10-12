import Link from "next/link";
import type { Monitor } from "@/lib/api";
import { duration, uptime } from "@/lib/format";
import { typeLabel } from "@/lib/monitor-types";
import { HeartbeatBar } from "@/app/ui/heartbeat-bar";
import { LiveDuration } from "@/app/ui/live-duration";
import { KindChip, monitorLabel } from "@/app/ui/monitor-name";
import { displayState, StatusBadge } from "@/app/ui/status";
import { TimeAgo } from "@/app/ui/time-ago";

/**
 * Monitors with their state, recent history and statistics. The monitors
 * must have been listed with their history; slots is how much of it a row
 * draws.
 */
export function MonitorTable({ monitors, slots }: { monitors: Monitor[]; slots: number }) {
  const window = monitors.length > 0 ? duration(monitors[0].window_seconds) : "";

  return (
    <div className="card overflow-x-auto">
      <table className="w-full table-fixed text-left text-sm">
        <colgroup>
          <col />
          <col className="w-36" />
          <col className="w-56" />
          <col className="w-28" />
          <col className="w-24" />
          <col className="w-28" />
        </colgroup>
        <thead className="border-b border-line text-xs text-muted">
          <tr>
            <th className="px-4 py-3 font-medium">Monitor</th>
            <th className="px-4 py-3 font-medium">Durum</th>
            <th className="px-4 py-3 font-medium">Son kontroller</th>
            <th className="px-4 py-3 text-right font-medium">Uptime ({window})</th>
            <th className="px-4 py-3 text-right font-medium">Ort. yanıt</th>
            <th className="px-4 py-3 text-right font-medium">Son kontrol</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {monitors.map((m) => {
            const label = monitorLabel(m);
            return (
              <tr key={m.id} className="hover:bg-sunken/50">
                <td className="px-4 py-3">
                  <div className="flex items-center gap-2">
                    <KindChip kind={label.kind ?? typeLabel(m.type)} />
                    <Link href={`/monitors/${m.id}`} className="truncate font-medium hover:underline">
                      {label.name}
                    </Link>
                  </div>
                  <div className="truncate text-xs text-muted" title={m.target}>
                    {m.target || m.description}
                  </div>
                </td>
                <td className="whitespace-nowrap px-4 py-3">
                  <StatusBadge state={displayState(m)} />
                  {m.open_incident && (
                    <div className="mt-0.5 text-xs text-muted">
                      <LiveDuration since={m.open_incident.started_at} />
                    </div>
                  )}
                  {m.cert_warning && <div className="mt-0.5 text-xs text-warn">Sertifika süresi doluyor</div>}
                </td>
                <td className="px-4 py-3">
                  <div className="[&>div]:h-5 [&>div]:gap-[2px]">
                    <HeartbeatBar results={m.history} slots={slots} />
                  </div>
                </td>
                <td className="px-4 py-3 text-right tabular-nums">{uptime(m.uptime_percent)}</td>
                <td className="px-4 py-3 text-right tabular-nums">
                  {/* The average covers successful checks; without any there is none. */}
                  {m.uptime_percent ? `${m.avg_latency_ms} ms` : "–"}
                </td>
                <td className="whitespace-nowrap px-4 py-3 text-right text-muted">
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
