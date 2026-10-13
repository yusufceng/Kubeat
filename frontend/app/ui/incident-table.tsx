import Link from "next/link";
import type { Incident } from "@/lib/api";
import { span } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import { LiveDuration } from "@/app/ui/live-duration";
import { LocalTime } from "@/app/ui/local-time";

export async function IncidentTable({
  incidents,
  showMonitor,
}: {
  incidents: Incident[];
  showMonitor: boolean;
}) {
  const t = await getT();
  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-line text-xs text-muted">
          <tr>
            {showMonitor && <th className="px-4 py-2 font-medium">Monitor</th>}
            <th className="px-4 py-2 font-medium">{t("incidentTable.kind")}</th>
            <th className="px-4 py-2 font-medium">{t("incidentTable.status")}</th>
            <th className="px-4 py-2 font-medium">{t("incidentTable.started")}</th>
            <th className="px-4 py-2 text-right font-medium">{t("incidentTable.duration")}</th>
            <th className="px-4 py-2 font-medium">{t("incidentTable.cause")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {incidents.map((i) => (
            <tr key={i.id}>
              {showMonitor && (
                <td className="px-4 py-2">
                  <Link href={`/monitors/${i.monitor_id}`} className="font-medium hover:underline">
                    {i.monitor_name}
                  </Link>
                </td>
              )}
              <td className="whitespace-nowrap px-4 py-2">
                {t(i.kind === "cert" ? "channelEvent.cert" : "channelEvent.down")}
              </td>
              <td className="whitespace-nowrap px-4 py-2">
                {i.resolved_at ? (
                  <span className="text-muted">{t("incidentTable.resolved")}</span>
                ) : (
                  <span
                    className={`font-medium ${
                      i.kind === "cert"
                        ? "text-warn"
                        : "text-down"
                    }`}
                  >
                    {t("incidentTable.ongoing")}
                  </span>
                )}
              </td>
              <td className="whitespace-nowrap px-4 py-2 tabular-nums"><LocalTime iso={i.started_at} /></td>
              <td className="whitespace-nowrap px-4 py-2 text-right tabular-nums">
                {i.resolved_at ? (
                  span(new Date(i.resolved_at).getTime() - new Date(i.started_at).getTime(), t)
                ) : (
                  <LiveDuration since={i.started_at} />
                )}
              </td>
              <td className="px-4 py-2 font-mono text-xs text-muted">{i.cause}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
