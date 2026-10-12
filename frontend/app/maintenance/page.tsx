import Link from "next/link";
import { Suspense } from "react";
import { listMaintenance, listMonitors, type Maintenance, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { BackendError } from "@/app/ui/backend-error";
import { LocalTime } from "@/app/ui/local-time";
import {
  DeleteMaintenanceButton,
  MaintenanceForm,
  type ScopeOption,
} from "@/app/ui/maintenance-forms";
import { monitorLabel } from "@/app/ui/monitor-name";

const stateLabels: Record<Maintenance["state"], { label: string; className: string }> = {
  active: { label: "Sürüyor", className: "font-medium text-emerald-700 dark:text-emerald-400" },
  upcoming: { label: "Planlandı", className: "" },
  past: { label: "Bitti", className: "text-zinc-500" },
};

function Scope({ window }: { window: Maintenance }) {
  if (window.monitor_id !== null) {
    return (
      <Link href={`/monitors/${window.monitor_id}`} className="hover:underline">
        {window.monitor_name}
      </Link>
    );
  }
  if (window.namespace) {
    return (
      <>
        <span className="text-zinc-500">namespace</span> {window.namespace}
      </>
    );
  }
  return <>Tüm monitor&apos;lar</>;
}

async function MaintenanceList() {
  let windows: Maintenance[];
  let monitors: Monitor[];
  try {
    [windows, monitors] = await Promise.all([
      listMaintenance(config.maintenanceLimit),
      listMonitors(),
    ]);
  } catch {
    return <BackendError what="Bakım pencereleri" />;
  }

  const namespaces = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  const scopes: ScopeOption[] = [
    { value: "all", label: "Tüm monitor'lar" },
    ...namespaces.map((ns) => ({ value: `ns:${ns}`, label: `Namespace: ${ns}` })),
    ...monitors.map((m) => ({
      value: `mon:${m.id}`,
      label: `Monitor: ${m.namespace ? `${m.namespace} / ` : ""}${monitorLabel(m).name}`,
    })),
  ];

  return (
    <>
      {windows.length === 0 ? (
        <p className="rounded-lg border border-dashed border-black/15 p-8 text-center text-sm text-zinc-500 dark:border-white/20">
          Bakım penceresi yok.
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/15">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-black/10 text-xs text-zinc-500 dark:border-white/15">
              <tr>
                <th className="px-4 py-2 font-medium">Açıklama</th>
                <th className="px-4 py-2 font-medium">Kapsam</th>
                <th className="px-4 py-2 font-medium">Başlangıç</th>
                <th className="px-4 py-2 font-medium">Bitiş</th>
                <th className="px-4 py-2 font-medium">Durum</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody className="divide-y divide-black/5 dark:divide-white/10">
              {windows.map((w) => (
                <tr key={w.id}>
                  <td className="px-4 py-2 font-medium">{w.name}</td>
                  <td className="px-4 py-2">
                    <Scope window={w} />
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                    <LocalTime iso={w.starts_at} seconds={false} />
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                    <LocalTime iso={w.ends_at} seconds={false} />
                  </td>
                  <td className={`whitespace-nowrap px-4 py-2 ${stateLabels[w.state].className}`}>
                    {stateLabels[w.state].label}
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex justify-end">
                      <DeleteMaintenanceButton
                        id={w.id}
                        label={w.state === "active" ? "Şimdi bitir" : "Sil"}
                      />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <section className="rounded-lg border border-black/10 p-5 dark:border-white/15">
        <h2 className="mb-4 text-sm font-semibold">Yeni bakım penceresi</h2>
        <MaintenanceForm scopes={scopes} durations={config.maintenanceDurations} />
      </section>
    </>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Bakım</h1>
        <p className="mt-1 text-sm text-zinc-500">
          Bakım penceresi boyunca kapsamdaki monitor&apos;lar kontrol edilmeye devam eder, ama yeni
          olay açılmaz ve bildirim gitmez. Pencere bittiğinde hedef hâlâ erişilemiyorsa olay o zaman
          açılır.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <MaintenanceList />
      </Suspense>
    </main>
  );
}
