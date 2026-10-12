import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import { ApiError, getMonitor, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { manualGroups, workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { MonitorForm } from "@/app/ui/monitor-form";
import { monitorLabel } from "@/app/ui/monitor-name";

async function EditMonitor({ params }: { params: Promise<{ id: string }> }) {
  const id = Number((await params).id);
  if (!Number.isInteger(id) || id <= 0) notFound();

  let monitor: Monitor;
  try {
    monitor = await getMonitor(id);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return <BackendError what="Monitor ayarları" err={err} />;
  }
  const name = monitorLabel(monitor).name;
  const groups = await workspaceMonitors().then(manualGroups, () => []);

  return (
    <>
      <header>
        <p className="mb-1.5 text-sm text-muted">
          <Link href="/" className="hover:text-ink">
            Genel bakış
          </Link>{" "}
          /{" "}
          <Link href={`/monitors/${monitor.id}`} className="hover:text-ink">
            {name}
          </Link>{" "}
          / Düzenle
        </p>
        <h1 className="text-2xl font-semibold tracking-tight">{name}</h1>
      </header>
      <MonitorForm monitor={monitor} defaults={config.newMonitor} groups={groups} />
    </>
  );
}

export default function Page({ params }: PageProps<"/monitors/[id]/edit">) {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
      <EditMonitor params={params} />
    </Suspense>
  );
}
