import Link from "next/link";
import { connection } from "next/server";
import { Suspense } from "react";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT, titled } from "@/lib/i18n/server";
import { manualGroups, workspaceMonitors } from "@/lib/workspace";
import { MonitorForm } from "@/app/ui/monitor-form";
import { CrumbTrail } from "@/app/ui/nav";
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

export const generateMetadata = titled("monitorForm.new.title");

export default async function Page() {
  const t = await getT();
  const overview = { label: t("monitorForm.breadcrumb.overview"), href: "/" };
  return (
    <div className="mon">
      <Crumbs items={[overview, { label: t("monitorForm.new.title") }]} />
      <header>
        {/* The context bar carries the breadcrumb on a wide screen; here, the steps above the title. */}
        <CrumbTrail items={[overview]} here={false} className="mb-2 lg:hidden" />
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
