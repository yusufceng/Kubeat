import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import type { Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { MANUAL_LABEL, namespaceFromSlug } from "@/lib/namespaces";
import { countStates, isProblem, workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { MonitorTable } from "@/app/ui/monitor-table";
import { displayState } from "@/app/ui/status";

async function Namespace({ params }: { params: Promise<{ name: string }> }) {
  const namespace = namespaceFromSlug((await params).name);

  let monitors: Monitor[];
  try {
    monitors = (await workspaceMonitors()).filter((m) => m.namespace === namespace);
  } catch (err) {
    return <BackendError what="Monitor durumları" err={err} />;
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
            Genel bakış
          </Link>{" "}
          / {namespace ? "Namespace" : "Grup"}
        </p>
        <h1 className="text-2xl font-semibold tracking-tight">{namespace || MANUAL_LABEL}</h1>
        <p className="mt-1 text-sm text-muted">
          {counts.up}/{counts.total} çalışıyor
          {bad > 0 && <span className="font-semibold text-down"> · {bad} erişilemiyor</span>}
          {counts.verifying > 0 && <span className="text-warn"> · {counts.verifying} doğrulanıyor</span>}
          {counts.maintenance > 0 && <span className="text-maint"> · {counts.maintenance} bakımda</span>}
        </p>
      </header>
      <MonitorTable monitors={ordered} slots={config.listBeats} />
    </>
  );
}

export default function Page({ params }: PageProps<"/namespaces/[name]">) {
  return (
    <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
      <Namespace params={params} />
    </Suspense>
  );
}
