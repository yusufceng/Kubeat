import Link from "next/link";
import { connection } from "next/server";
import { Suspense } from "react";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
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

export default function Page() {
  return (
    <>
      <header>
        <p className="mb-1.5 text-sm text-muted">
          <Link href="/" className="hover:text-ink">
            Genel bakış
          </Link>{" "}
          / Yeni monitor
        </p>
        <h1 className="text-2xl font-semibold tracking-tight">Yeni monitor</h1>
        <p className="mt-1 text-sm text-muted">
          Cluster&apos;daki servisleri tek tek girmek yerine{" "}
          <Link href="/discovery" className="text-accent underline">
            keşifle topluca ekleyebilirsiniz
          </Link>
          .
        </p>
      </header>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <NewMonitor />
      </Suspense>
    </>
  );
}
