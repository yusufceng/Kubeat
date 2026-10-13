import Link from "next/link";
import { connection } from "next/server";
import { Suspense } from "react";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT } from "@/lib/i18n/server";
import { manualGroups, workspaceMonitors } from "@/lib/workspace";
import { MonitorForm } from "@/app/ui/monitor-form";
import { ReadOnlyNotice } from "@/app/ui/read-only-notice";

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
    <>
      <header>
        <p className="mb-1.5 text-sm text-muted">
          <Link href="/" className="hover:text-ink">
            {t("monitorForm.breadcrumb.overview")}
          </Link>{" "}
          / {t("monitorForm.new.title")}
        </p>
        <h1 className="text-2xl font-semibold tracking-tight">{t("monitorForm.new.title")}</h1>
        <p className="mt-1 text-sm text-muted">
          {t("monitorForm.new.discoveryLead")}{" "}
          <Link href="/discovery" className="text-accent underline">
            {t("monitorForm.new.discoveryLink")}
          </Link>
          .
        </p>
      </header>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <NewMonitor />
      </Suspense>
    </>
  );
}
