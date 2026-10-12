import Link from "next/link";
import type { Incident } from "@/lib/api";
import { span } from "@/lib/format";
import { LiveDuration } from "@/app/ui/live-duration";
import { LocalTime } from "@/app/ui/local-time";

export function IncidentTable({
  incidents,
  showMonitor,
}: {
  incidents: Incident[];
  showMonitor: boolean;
}) {
  return (
    <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/15">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-black/10 text-xs text-zinc-500 dark:border-white/15">
          <tr>
            {showMonitor && <th className="px-4 py-2 font-medium">Monitor</th>}
            <th className="px-4 py-2 font-medium">Tür</th>
            <th className="px-4 py-2 font-medium">Durum</th>
            <th className="px-4 py-2 font-medium">Başlangıç</th>
            <th className="px-4 py-2 text-right font-medium">Süre</th>
            <th className="px-4 py-2 font-medium">Neden</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-black/5 dark:divide-white/10">
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
                {i.kind === "cert" ? "Sertifika" : "Kesinti"}
              </td>
              <td className="whitespace-nowrap px-4 py-2">
                {i.resolved_at ? (
                  <span className="text-zinc-500">Çözüldü</span>
                ) : (
                  <span
                    className={`font-medium ${
                      i.kind === "cert"
                        ? "text-amber-700 dark:text-amber-400"
                        : "text-red-700 dark:text-red-400"
                    }`}
                  >
                    Devam ediyor
                  </span>
                )}
              </td>
              <td className="whitespace-nowrap px-4 py-2 tabular-nums"><LocalTime iso={i.started_at} /></td>
              <td className="whitespace-nowrap px-4 py-2 text-right tabular-nums">
                {i.resolved_at ? (
                  span(new Date(i.resolved_at).getTime() - new Date(i.started_at).getTime())
                ) : (
                  <LiveDuration since={i.started_at} />
                )}
              </td>
              <td className="px-4 py-2 font-mono text-xs text-zinc-500">{i.cause}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
