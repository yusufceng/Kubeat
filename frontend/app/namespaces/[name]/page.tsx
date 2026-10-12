import Link from "next/link";
import { notFound } from "next/navigation";
import { Suspense } from "react";
import { listMonitors, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { MANUAL_LABEL, namespaceFromSlug } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { countStates, isProblem, MonitorTable, StateStats } from "@/app/ui/monitor-table";

async function Namespace({ params }: { params: Promise<{ name: string }> }) {
  const namespace = namespaceFromSlug((await params).name);

  let monitors: Monitor[];
  try {
    monitors = await listMonitors({ namespace, history: config.historyLimit });
  } catch (err) {
    return <BackendError what="Monitor durumları" err={err} />;
  }
  if (monitors.length === 0) notFound();

  // Problems first; otherwise in the order the backend gives.
  const ordered = [...monitors.filter(isProblem), ...monitors.filter((m) => !isProblem(m))];

  return (
    <>
      <h1 className="text-2xl font-semibold tracking-tight">
        {namespace ? (
          <>
            <span className="font-normal text-zinc-500">namespace</span> {namespace}
          </>
        ) : (
          MANUAL_LABEL
        )}
      </h1>
      <StateStats counts={countStates(monitors)} />
      <MonitorTable monitors={ordered} />
    </>
  );
}

export default function Page({ params }: PageProps<"/namespaces/[name]">) {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <Link href="/" className="text-sm text-zinc-500 hover:underline">
        ← Tüm monitor&apos;lar
      </Link>
      <LiveRefresh />
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <Namespace params={params} />
      </Suspense>
    </main>
  );
}
