import Link from "next/link";
import type { Monitor } from "@/lib/api";
import { toFleetItem } from "@/lib/fleet";
import { duration, milliseconds, uptime } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import { splitName } from "@/lib/kinds";
import { uptimeTone, type UptimeTones } from "@/lib/uptime-tone";
import { HeartbeatBar } from "@/app/ui/heartbeat-bar";
import { LiveDuration } from "@/app/ui/live-duration";
import { CertChip } from "@/app/ui/monitor-row";
import { stateStyles } from "@/app/ui/status";
import { StatusGlyph } from "@/app/ui/status-glyph";
import { TimeAgo } from "@/app/ui/time-ago";

/**
 * Monitors as a table of 36px rows: the state, the name with its kind, the
 * address, the recent checks, uptime, response time and the last check.
 * Names never wrap; figures are right-aligned. Where the page is narrow
 * the address goes first, then response time and the last check. The
 * monitors must have been listed with their history; slots is how much of
 * it a row draws.
 */
export async function MonitorTable({
  monitors,
  slots,
  tones,
}: {
  monitors: Monitor[];
  slots: number;
  tones: UptimeTones;
}) {
  const t = await getT();
  const window = monitors.length > 0 ? duration(monitors[0].window_seconds, t) : "";

  return (
    <div className="panel fl-table-wrap">
      <table className="fl-table">
        <thead>
          <tr>
            <th scope="col">{t("monitorsList.table.monitor")}</th>
            <th scope="col" className="fl-col-state">
              {t("monitorsList.table.state")}
            </th>
            <th scope="col" className="fl-col-target">
              {t("monitorsList.table.target")}
            </th>
            <th scope="col" className="fl-col-beats">
              {t("monitorsList.table.recentChecks")}
            </th>
            <th scope="col" className="fl-col-num fl-col-uptime">
              {t("monitorsList.table.uptime", { window })}
            </th>
            <th scope="col" className="fl-col-num fl-col-latency">
              {t("monitorsList.table.avgLatency")}
            </th>
            <th scope="col" className="fl-col-num fl-col-last">
              {t("monitorsList.table.lastCheck")}
            </th>
          </tr>
        </thead>
        <tbody>
          {monitors.map((m) => {
            const item = toFleetItem(m);
            const style = stateStyles[item.state];
            const [name, rest] = splitName(item.name, item.derived);
            return (
              <tr key={m.id}>
                <td>
                  <Link href={`/monitors/${m.id}`} className="fl-table-name">
                    <StatusGlyph kind={style.glyph} live={item.state === "down"} />
                    <span className="fl-name-text" title={item.name}>
                      {name}
                      {rest && <span className="text-ink-3">{rest}</span>}
                    </span>
                    {/* As in the list, a certificate near its end takes the kind's place. */}
                    {item.certDays !== null ? <CertChip days={item.certDays} /> : <span className="fl-kind">{item.kindShort}</span>}
                  </Link>
                </td>
                <td className={`fl-col-state ${style.text}`}>
                  {t(style.label)}
                  {item.downSince && (
                    <span className="fl-state-for tabular-nums">
                      {" · "}
                      <LiveDuration since={item.downSince} />
                    </span>
                  )}
                </td>
                <td className="fl-col-target">
                  <span className="type-mono block truncate text-ink-3" title={m.target || undefined}>
                    {m.target || t("common.none")}
                  </span>
                </td>
                <td className="fl-col-beats">
                  <HeartbeatBar results={m.history} slots={slots} height={16} paused={!m.enabled} />
                </td>
                <td className={`fl-col-num fl-col-uptime ${m.enabled ? uptimeTone(m.uptime_percent, tones) : "text-ink-3"}`}>
                  {uptime(m.uptime_percent, t)}
                </td>
                <td className="fl-col-num fl-col-latency">
                  {/* The average covers successful checks; without any there is none. */}
                  {m.uptime_percent ? milliseconds(m.avg_latency_ms, t) : t("common.none")}
                </td>
                <td className="fl-col-num fl-col-last text-ink-3">
                  {m.last_result ? <TimeAgo iso={m.last_result.checked_at} /> : t("common.none")}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
