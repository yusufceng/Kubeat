import { getWorkload, type Workload } from "@/lib/api";
import { cpu, memory } from "@/lib/format";
import { LiveDuration } from "@/app/ui/live-duration";
import { LocalTime } from "@/app/ui/local-time";

/** What the cluster recorded about the pods lately: the "why" behind a restart or a pod that will not start. */
function Events({ workload }: { workload: Workload }) {
  if (workload.events_reason) {
    return <p className="mt-2 text-xs text-muted">Olaylar okunamadı: {workload.events_reason}</p>;
  }
  if (workload.events.length === 0) {
    return <p className="mt-2 text-xs text-muted">Cluster bu pod&apos;lar için yakın zamanda olay kaydetmemiş.</p>;
  }
  return (
    <div className="mt-3">
      <h3 className="mb-1.5 text-xs font-semibold text-muted">Cluster olayları</h3>
      <ul className="card divide-y divide-line text-sm">
        {workload.events.map((e, i) => (
          <li key={i} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-4 py-2">
            <span className="whitespace-nowrap text-xs tabular-nums text-muted">
              <LocalTime iso={e.at} />
            </span>
            <span className={`text-xs font-medium ${e.type === "Warning" ? "text-warn" : "text-muted"}`}>
              {e.reason}
              {e.count > 1 && ` ×${e.count}`}
            </span>
            <span className="font-mono text-xs text-muted">{e.object}</span>
            <span className="min-w-0 flex-1 break-words">{e.message}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * The pods behind a discovered monitor, read live from the cluster. A check
 * through a Service reaches one pod at a time; this shows the rest.
 */
export async function PodsPanel({ monitorId }: { monitorId: number }) {
  let workload: Workload;
  try {
    workload = await getWorkload(monitorId);
  } catch {
    return (
      <p className="text-sm text-muted">Pod bilgisi şu anda cluster&apos;dan okunamıyor.</p>
    );
  }
  if (!workload.available) {
    return (
      <p className="text-sm text-muted">
        Bu monitor&apos;un arkasındaki pod&apos;lar belirlenemiyor.
        <span className="mt-1 block font-mono text-xs">{workload.reason}</span>
      </p>
    );
  }
  if (workload.total === 0) {
    return (
      <p
        role="alert"
        className="rounded-lg border border-down/40 bg-down/8 p-4 text-sm text-down"
      >
        {workload.service ? (
          <>
            <span className="font-mono">{workload.service}</span> servisinin arkasında hiç pod yok.
          </>
        ) : (
          "Bu workload'un hiç pod'u yok."
        )}
      </p>
    );
  }

  const allReady = workload.ready === workload.total;

  return (
    <>
      <p className="mb-2 text-sm">
        <span
          className={`font-semibold ${
            allReady
              ? "text-up"
              : "text-warn"
          }`}
        >
          {workload.ready}/{workload.total} pod hazır
        </span>
        {workload.service && (
          <span className="text-muted">
            {" "}
            · servis <span className="font-mono">{workload.service}</span>
          </span>
        )}
      </p>
      <div className="card overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-line text-xs text-muted">
            <tr>
              <th className="px-4 py-2 font-medium">Pod</th>
              <th className="px-4 py-2 font-medium">Durum</th>
              <th className="px-4 py-2 text-right font-medium">Container</th>
              <th className="px-4 py-2 text-right font-medium">Restart</th>
              <th className="px-4 py-2 text-right font-medium">Yaş</th>
              <th className="px-4 py-2 text-right font-medium">CPU</th>
              <th className="px-4 py-2 text-right font-medium">Bellek</th>
              <th className="px-4 py-2 font-medium">Node</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {workload.pods.map((p) => (
              <tr key={p.name}>
                <td className="px-4 py-2 font-mono text-xs">{p.name}</td>
                <td className="whitespace-nowrap px-4 py-2">
                  <span className="inline-flex items-center gap-2">
                    <span
                      className={`h-2.5 w-2.5 rounded-full ${p.ready ? "bg-up" : "bg-down"}`}
                      aria-hidden
                    />
                    <span
                      className={
                        p.ready ? undefined : "font-medium text-down"
                      }
                    >
                      {p.ready ? "Hazır" : "Hazır değil"}
                    </span>
                    {(!p.ready || p.status !== "Running") && (
                      <span className="font-mono text-xs text-muted">{p.status}</span>
                    )}
                  </span>
                </td>
                <td className="px-4 py-2 text-right tabular-nums">
                  {p.ready_containers}/{p.containers}
                </td>
                <td
                  className={`px-4 py-2 text-right tabular-nums ${
                    p.restarts > 0 ? "text-warn" : ""
                  }`}
                >
                  {p.restarts}
                </td>
                <td className="whitespace-nowrap px-4 py-2 text-right tabular-nums text-muted">
                  {p.started_at ? <LiveDuration since={p.started_at} /> : "–"}
                </td>
                <td className="px-4 py-2 text-right tabular-nums">
                  {p.cpu_cores !== null ? cpu(p.cpu_cores) : "–"}
                </td>
                <td className="px-4 py-2 text-right tabular-nums">
                  {p.memory_bytes !== null ? memory(p.memory_bytes) : "–"}
                </td>
                <td className="px-4 py-2 text-xs text-muted">{p.node || "–"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!workload.metrics.available && (
        <p className="mt-2 text-xs text-muted">
          CPU ve bellek gösterilemiyor: {workload.metrics.reason}
        </p>
      )}
      <Events workload={workload} />
    </>
  );
}
