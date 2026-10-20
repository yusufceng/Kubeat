import Link from "next/link";
import { connection } from "next/server";
import { Suspense } from "react";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT } from "@/lib/i18n/server";
import { manualGroups, workspaceMonitors } from "@/lib/workspace";
import { MonitorForm } from "@/app/ui/monitor-form";
import { ReadOnlyNotice } from "@/app/ui/read-only-notice";
import { Crumbs } from "@/app/ui/shell-state";

/** The form, with the defaults of the running server's environment. */
async function NewMonitor() {
  await connection();
  if (!(await currentUser()).can_edit) return <ReadOnlyNotice />;
  // The form works without the suggestions, should the backend be away.
  const groups = await workspaceMonitors().then(manualGroups, () => []);
  return <MonitorForm defaults={config.newMonitor} groups={groups} />;
}

export default async function Page() {
  const t = await getT();
  return (
    <div className="mon">
      <Crumbs items={[{ label: t("monitorForm.breadcrumb.overview"), href: "/" }, { label: t("monitorForm.new.title") }]} />
      <header>
        {/* The context bar carries the breadcrumb on a wide screen. */}
        <p className="mb-2 flex items-center gap-1.5 text-[13px] text-ink-3 lg:hidden">
          <Link href="/" className="hover:text-ink">
            {t("monitorForm.breadcrumb.overview")}
          </Link>
          <span aria-hidden>/</span>
          <span>{t("monitorForm.new.title")}</span>
        </p>
        <h1 className="type-display-s @min-[42rem]:type-display">{t("monitorForm.new.title")}</h1>
        <p className="mt-1.5 text-ink-2">
          {t("monitorForm.new.discoveryLead")}{" "}
          <Link href="/discovery" className="font-medium text-accent-text hover:underline">
            {t("monitorForm.new.discoveryLink")}
          </Link>
          .
        </p>
      </header>
      <Suspense fallback={<p className="text-[13px] text-ink-3">{t("common.loading")}</p>}>
        <NewMonitor />
      </Suspense>
    </div>
  );
}
