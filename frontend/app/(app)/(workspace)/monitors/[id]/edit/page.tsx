import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import { ApiError, getMonitor, type Monitor } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT } from "@/lib/i18n/server";
import { kubectlName, typeWords } from "@/lib/monitor-page";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { manualGroups, workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { MonitorForm } from "@/app/ui/monitor-form";
import { ReadOnlyNotice } from "@/app/ui/read-only-notice";
import { Crumbs } from "@/app/ui/shell-state";

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
  const me = await currentUser();
  const groups = me.can_edit ? await workspaceMonitors().then(manualGroups, () => []) : [];
  const object = kubectlName(monitor.source);

  return (
    <>
      <Crumbs
        items={[
          { label: t("monitorForm.breadcrumb.overview"), href: "/" },
          { label: monitor.namespace || t(MANUAL_LABEL), href: namespaceHref(monitor.namespace), mono: monitor.namespace !== "" },
          { label: monitor.name, href: `/monitors/${monitor.id}` },
          { label: t("common.edit") },
        ]}
      />
      <header>
        {/* The context bar carries the breadcrumb on a wide screen. */}
        <p className="mb-2 flex flex-wrap items-center gap-1.5 text-[13px] text-ink-3 lg:hidden">
          <Link href="/" className="hover:text-ink">
            {t("monitorForm.breadcrumb.overview")}
          </Link>
          <span aria-hidden>/</span>
          <Link href={`/monitors/${monitor.id}`} className="hover:text-ink">
            {monitor.name}
          </Link>
          <span aria-hidden>/</span>
          <span>{t("common.edit")}</span>
        </p>
        <div className="mon-title-row">
          <h1 className="mon-title type-display-s @min-[42rem]:type-display">{monitor.name}</h1>
          <span className="mon-kind" title={`${t("monitorPage.kind.title")}: ${object ? `${object} · ` : ""}${typeWords[monitor.type]}`}>
            {object && (
              <>
                <span>{object}</span>
                <i aria-hidden />
              </>
            )}
            <span>{typeWords[monitor.type]}</span>
          </span>
        </div>
        <p className="mon-desc">{t("monitorForm.edit.title")}</p>
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
    <div className="mon">
      <Suspense fallback={<p className="text-[13px] text-ink-3">{t("common.loading")}</p>}>
        <EditMonitor params={params} />
      </Suspense>
    </div>
  );
}
