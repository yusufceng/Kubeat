import { Suspense } from "react";
import { type Incident, listIncidents } from "@/lib/api";
import { config } from "@/lib/config";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { BackendError } from "@/app/ui/backend-error";
import { IncidentTable } from "@/app/ui/incident-table";

async function Incidents() {
  let incidents: Incident[];
  try {
    incidents = await listIncidents(config.incidentsLimit);
  } catch (err) {
    return <BackendError what="Olaylar" err={err} />;
  }

  if (incidents.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-black/15 p-8 text-center text-sm text-zinc-500 dark:border-white/20">
        Henüz olay yok. Bir monitor alarm eşiği kadar art arda başarısız olursa burada görünür.
      </p>
    );
  }
  const open = incidents.filter((i) => !i.resolved_at).length;

  return (
    <>
      <p className="text-sm text-zinc-500">
        {open > 0 ? `${open} olay devam ediyor.` : "Devam eden olay yok."} Son {incidents.length}{" "}
        olay listeleniyor.
      </p>
      <IncidentTable incidents={incidents} showMonitor />
    </>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <h1 className="text-2xl font-semibold tracking-tight">Olaylar</h1>
      <LiveRefresh />
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <Incidents />
      </Suspense>
    </main>
  );
}
