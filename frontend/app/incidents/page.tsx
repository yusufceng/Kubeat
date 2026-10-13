import { Suspense } from "react";
import { type Incident, listIncidents } from "@/lib/api";
import { config } from "@/lib/config";
import { getT } from "@/lib/i18n/server";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { BackendError } from "@/app/ui/backend-error";
import { IncidentTable } from "@/app/ui/incident-table";

async function Incidents() {
  const t = await getT();
  let incidents: Incident[];
  try {
    incidents = await listIncidents(config.incidentsLimit);
  } catch (err) {
    return <BackendError what={t("nav.incidents")} err={err} />;
  }

  if (incidents.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-muted">
        {t("incidents.empty")}
      </p>
    );
  }
  const open = incidents.filter((i) => !i.resolved_at).length;

  return (
    <>
      <p className="text-sm text-muted">
        {open > 0 ? t.n("incidents.open", open) : t("incidents.noneOpen")}{" "}
        {t.n("incidents.listed", incidents.length)}
      </p>
      <IncidentTable incidents={incidents} showMonitor />
    </>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <h1 className="text-2xl font-semibold tracking-tight">{t("nav.incidents")}</h1>
      <LiveRefresh />
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <Incidents />
      </Suspense>
    </main>
  );
}
