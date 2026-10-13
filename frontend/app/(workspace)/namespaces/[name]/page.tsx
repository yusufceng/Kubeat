import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import type { Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { getT } from "@/lib/i18n/server";
import { MANUAL_LABEL, namespaceFromSlug } from "@/lib/namespaces";
import { countStates, isProblem, workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { MonitorTable } from "@/app/ui/monitor-table";
import { displayState } from "@/app/ui/status";

async function Namespace({ params }: { params: Promise<{ name: string }> }) {
  const t = await getT();
  const namespace = namespaceFromSlug((await params).name);

  let monitors: Monitor[];
  try {
    monitors = (await workspaceMonitors()).filter((m) => m.namespace === namespace);
  } catch (err) {
    return <BackendError what={t("monitorsList.namespace.what")} err={err} />;
  }
  if (monitors.length === 0) notFound();

  // Problems first; otherwise in the order the backend gives.
  const failing = (m: Monitor) => isProblem(displayState(m));
  const ordered = [...monitors.filter(failing), ...monitors.filter((m) => !failing(m))];
  const counts = countStates(monitors);
  const bad = counts.down + counts.recovering;

  return (
    <>
      <header>
        <p className="mb-1.5 text-sm text-muted">
          <Link href="/" className="hover:text-ink">
            {t("monitorsList.overview.title")}
          </Link>{" "}
          / {namespace ? t("monitorsList.namespace.kind.namespace") : t("monitorsList.namespace.kind.group")}
        </p>
        <h1 className="text-2xl font-semibold tracking-tight">{namespace || t(MANUAL_LABEL)}</h1>
        <p className="mt-1 text-sm text-muted">
          {t("monitorsList.namespace.up", { up: counts.up, total: counts.total })}
          {bad > 0 && <span className="font-semibold text-down"> · {t("monitorsList.namespace.down", { count: bad })}</span>}
          {counts.verifying > 0 && (
            <span className="text-warn"> · {t("monitorsList.namespace.verifying", { count: counts.verifying })}</span>
          )}
          {counts.maintenance > 0 && (
            <span className="text-maint"> · {t("monitorsList.namespace.maintenance", { count: counts.maintenance })}</span>
          )}
        </p>
      </header>
      <MonitorTable monitors={ordered} slots={config.listBeats} />
    </>
  );
}

export default async function Page({ params }: PageProps<"/namespaces/[name]">) {
  const t = await getT();
  return (
    <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
      <Namespace params={params} />
    </Suspense>
  );
}
