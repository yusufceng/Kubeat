import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import { ApiError, getMonitor, type Monitor } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT } from "@/lib/i18n/server";
import { manualGroups, workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { MonitorForm } from "@/app/ui/monitor-form";
import { monitorLabel } from "@/app/ui/monitor-name";
import { ReadOnlyNotice } from "@/app/ui/read-only-notice";

async function EditMonitor({ params }: { params: Promise<{ id: string }> }) {
  const id = Number((await params).id);
  if (!Number.isInteger(id) || id <= 0) notFound();

  const t = await getT();
  let monitor: Monitor;
  try {
    monitor = await getMonitor(id);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return <BackendError what={t("monitorForm.edit.what")} err={err} />;
  }
  const name = monitorLabel(monitor).name;
  const me = await currentUser();
  const groups = me.can_edit ? await workspaceMonitors().then(manualGroups, () => []) : [];

  return (
    <>
      <header>
        <p className="mb-1.5 text-sm text-muted">
          <Link href="/" className="hover:text-ink">
            {t("monitorForm.breadcrumb.overview")}
          </Link>{" "}
          /{" "}
          <Link href={`/monitors/${monitor.id}`} className="hover:text-ink">
            {name}
          </Link>{" "}
          / {t("common.edit")}
        </p>
        <h1 className="text-2xl font-semibold tracking-tight">{name}</h1>
      </header>
      {me.can_edit ? (
        <MonitorForm monitor={monitor} defaults={config.newMonitor} groups={groups} />
      ) : (
        <ReadOnlyNotice />
      )}
    </>
  );
}

export default async function Page({ params }: PageProps<"/monitors/[id]/edit">) {
  const t = await getT();
  return (
    <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
      <EditMonitor params={params} />
    </Suspense>
  );
}
