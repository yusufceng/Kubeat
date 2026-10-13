import Link from "next/link";
import { Suspense } from "react";
import { listMaintenance, listMonitors, type Maintenance, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { currentUser } from "@/lib/auth";
import { BackendError } from "@/app/ui/backend-error";
import { LocalTime } from "@/app/ui/local-time";
import {
  DeleteMaintenanceButton,
  MaintenanceForm,
  type ScopeOption,
} from "@/app/ui/maintenance-forms";
import { monitorLabel } from "@/app/ui/monitor-name";
import { getT } from "@/lib/i18n/server";

/** The label is a message key; show it with t(). */
const stateLabels: Record<Maintenance["state"], { label: string; className: string }> = {
  active: { label: "maintenance.state.active", className: "font-medium text-up" },
  upcoming: { label: "maintenance.state.upcoming", className: "" }, // for a repeating window: its next occurrence
  past: { label: "maintenance.state.past", className: "text-muted" },
};

/** Message keys; show them with t(). */
const repeatLabels: Record<Maintenance["repeat"], string> = {
  "": "common.none",
  daily: "maintenance.repeat.daily",
  weekly: "maintenance.repeat.weekly",
  monthly: "maintenance.repeat.monthly",
};

async function Scope({ window }: { window: Maintenance }) {
  if (window.monitor_id !== null) {
    return (
      <Link href={`/monitors/${window.monitor_id}`} className="hover:underline">
        {window.monitor_name}
      </Link>
    );
  }
  const t = await getT();
  if (window.namespace) {
    return (
      <>
        <span className="text-muted">{t("maintenance.scope.namespace")}</span> {window.namespace}
      </>
    );
  }
  return <>{t("maintenance.scope.all")}</>;
}

async function MaintenanceList() {
  const me = await currentUser();
  const t = await getT();
  let windows: Maintenance[];
  let monitors: Monitor[];
  try {
    [windows, monitors] = await Promise.all([
      listMaintenance(config.maintenanceLimit),
      listMonitors(),
    ]);
  } catch (err) {
    return <BackendError what={t("maintenance.what")} err={err} />;
  }

  const namespaces = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  const scopes: ScopeOption[] = [
    { value: "all", label: t("maintenance.scope.all") },
    ...namespaces.map((ns) => ({ value: `ns:${ns}`, label: t("maintenance.scope.namespaceOption", { namespace: ns }) })),
    ...monitors.map((m) => ({
      value: `mon:${m.id}`,
      label: t("maintenance.scope.monitorOption", {
        monitor: `${m.namespace ? `${m.namespace} / ` : ""}${monitorLabel(m).name}`,
      }),
    })),
  ];

  return (
    <>
      {windows.length === 0 ? (
        <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-muted">
          {t("maintenance.empty")}
        </p>
      ) : (
        <div className="card overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-line text-xs text-muted">
              <tr>
                <th className="px-4 py-2 font-medium">{t("maintenance.name")}</th>
                <th className="px-4 py-2 font-medium">{t("maintenance.scope")}</th>
                <th className="px-4 py-2 font-medium">{t("maintenance.repeat")}</th>
                <th className="px-4 py-2 font-medium">{t("maintenance.start")}</th>
                <th className="px-4 py-2 font-medium">{t("maintenance.end")}</th>
                <th className="px-4 py-2 font-medium">{t("maintenance.status")}</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {windows.map((w) => (
                <tr key={w.id}>
                  <td className="px-4 py-2 font-medium">{w.name}</td>
                  <td className="px-4 py-2">
                    <Scope window={w} />
                  </td>
                  <td
                    className="whitespace-nowrap px-4 py-2"
                    title={w.repeat ? t("maintenance.timezoneTitle", { timezone: w.timezone }) : undefined}
                  >
                    {t(repeatLabels[w.repeat] ?? w.repeat)}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                    <LocalTime iso={w.starts_at} seconds={false} />
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                    <LocalTime iso={w.ends_at} seconds={false} />
                  </td>
                  <td className={`whitespace-nowrap px-4 py-2 ${stateLabels[w.state].className}`}>
                    {t(stateLabels[w.state].label)}
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex justify-end">
                      {w.repeat ? (
                        <DeleteMaintenanceButton
                          id={w.id}
                          label={t("maintenance.deleteSeries")}
                          confirm={t("maintenance.deleteSeriesConfirm", { name: w.name })}
                        />
                      ) : (
                        <DeleteMaintenanceButton
                          id={w.id}
                          label={w.state === "active" ? t("maintenance.endNow") : t("common.delete")}
                        />
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {me.can_edit && (
        <section className="card p-5">
          <h2 className="mb-4 text-sm font-semibold">{t("maintenance.new")}</h2>
          <MaintenanceForm scopes={scopes} durations={config.maintenanceDurations} />
        </section>
      )}
    </>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("nav.maintenance")}</h1>
        <p className="mt-1 text-sm text-muted">{t("maintenance.intro")}</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <MaintenanceList />
      </Suspense>
    </main>
  );
}
