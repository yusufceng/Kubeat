import Link from "next/link";
import { Suspense } from "react";
import { listMonitors, type Monitor } from "@/lib/api";
import { config } from "@/lib/config";
import { compareNamespaces, MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { AddMonitorForm } from "@/app/ui/add-monitor-form";
import { BackendError } from "@/app/ui/backend-error";
import { LiveRefresh } from "@/app/ui/live-refresh";
import {
  countStates,
  GroupSummary,
  isProblem,
  MonitorTable,
  StateStats,
} from "@/app/ui/monitor-table";

function byNamespace(monitors: Monitor[]): [string, Monitor[]][] {
  const groups = new Map<string, Monitor[]>();
  for (const m of monitors) groups.set(m.namespace, [...(groups.get(m.namespace) ?? []), m]);
  return [...groups.entries()].sort(([a], [b]) => compareNamespaces(a, b));
}

function NamespaceTitle({ namespace }: { namespace: string }) {
  return (
    <Link href={namespaceHref(namespace)} className="hover:underline">
      {namespace ? (
        <>
          <span className="font-normal text-zinc-500">namespace</span> {namespace}
        </>
      ) : (
        MANUAL_LABEL
      )}
    </Link>
  );
}

/** Every monitor, grouped by namespace. For a number of monitors that fits a page. */
function EveryMonitor({ monitors }: { monitors: Monitor[] }) {
  return byNamespace(monitors).map(([namespace, group]) => (
    <section key={namespace} aria-label={namespace || MANUAL_LABEL}>
      <div className="mb-2 flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-semibold">
          <NamespaceTitle namespace={namespace} />
        </h2>
        <GroupSummary counts={countStates(group)} />
      </div>
      <MonitorTable monitors={group} />
    </section>
  ));
}

/**
 * What needs attention, then one line per namespace. For more monitors than
 * anyone would read through; each namespace has its own page.
 */
function Overview({
  all,
  problems,
  problemCount,
}: {
  all: Monitor[];
  /** The listed problem monitors, with their history. */
  problems: Monitor[];
  problemCount: number;
}) {
  return (
    <>
      <section aria-label="Sorunlu monitor'lar">
        <h2 className="mb-2 text-sm font-semibold">
          Sorunlu monitor&apos;lar{problemCount > 0 && ` (${problemCount})`}
        </h2>
        {problemCount === 0 ? (
          <p className="rounded-lg border border-emerald-600/30 bg-emerald-600/5 p-4 text-sm text-emerald-800 dark:text-emerald-300">
            Şu anda başarısız olan monitor yok.
          </p>
        ) : (
          <>
            <MonitorTable monitors={problems} showNamespace />
            {problemCount > problems.length && (
              <p className="mt-2 text-xs text-zinc-500">
                İlk {problems.length} tanesi gösteriliyor; {problemCount - problems.length} sorunlu
                monitor daha var. Tamamı için namespace sayfalarına bakın.
              </p>
            )}
          </>
        )}
      </section>

      <section aria-label="Namespace'ler">
        <h2 className="mb-2 text-sm font-semibold">Namespace&apos;ler</h2>
        <div className="overflow-x-auto rounded-lg border border-black/10 dark:border-white/15">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-black/10 text-xs text-zinc-500 dark:border-white/15">
              <tr>
                <th className="px-4 py-2 font-medium">Namespace</th>
                <th className="px-4 py-2 text-right font-medium">Monitor</th>
                <th className="px-4 py-2 text-right font-medium">Çalışıyor</th>
                <th className="px-4 py-2 text-right font-medium">Erişilemiyor</th>
                <th className="px-4 py-2 text-right font-medium">Doğrulanıyor</th>
                <th className="px-4 py-2 text-right font-medium">Bekleniyor / durduruldu</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-black/5 tabular-nums dark:divide-white/10">
              {byNamespace(all).map(([namespace, group]) => {
                const counts = countStates(group);
                return (
                  <tr key={namespace}>
                    <td className="px-4 py-2 font-medium">
                      <NamespaceTitle namespace={namespace} />
                    </td>
                    <td className="px-4 py-2 text-right">{counts.total}</td>
                    <td className="px-4 py-2 text-right">{counts.up}</td>
                    <td
                      className={`px-4 py-2 text-right ${
                        counts.down > 0 ? "font-semibold text-red-600 dark:text-red-400" : ""
                      }`}
                    >
                      {counts.down}
                    </td>
                    <td
                      className={`px-4 py-2 text-right ${
                        counts.verifying > 0 ? "text-amber-700 dark:text-amber-400" : ""
                      }`}
                    >
                      {counts.verifying}
                    </td>
                    <td className="px-4 py-2 text-right text-zinc-500">{counts.idle}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}

async function Monitors() {
  let all: Monitor[];
  // The monitors drawn with a history strip: everything, or just the problems.
  let detailed: Monitor[];
  let problemCount = 0;
  let flat = true;
  try {
    all = await listMonitors();
    flat = all.length <= config.dashboardFlatLimit;
    if (flat) {
      detailed = all.length > 0 ? await listMonitors({ history: config.historyLimit }) : [];
    } else {
      const problems = all.filter(isProblem);
      problemCount = problems.length;
      const ids = problems.slice(0, config.problemLimit).map((m) => m.id);
      detailed = ids.length > 0 ? await listMonitors({ ids, history: config.historyLimit }) : [];
    }
  } catch (err) {
    return <BackendError what="Monitor durumları" err={err} />;
  }

  if (all.length === 0) {
    return (
      <p className="rounded-lg border border-dashed border-black/15 p-8 text-center text-sm text-zinc-500 dark:border-white/20">
        Henüz monitor yok.{" "}
        <Link href="/discovery" className="underline">
          Keşif
        </Link>{" "}
        ile cluster&apos;daki servisleri bulun ya da aşağıdaki formdan elle ekleyin.
      </p>
    );
  }

  return (
    <>
      <StateStats counts={countStates(all)} />
      {flat ? (
        <EveryMonitor monitors={detailed} />
      ) : (
        <Overview all={all} problems={detailed} problemCount={problemCount} />
      )}
    </>
  );
}

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <h1 className="text-2xl font-semibold tracking-tight">Monitor&apos;lar</h1>
      <LiveRefresh />
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <Monitors />
      </Suspense>
      <section className="rounded-lg border border-black/10 p-5 dark:border-white/15">
        <h2 className="mb-4 text-sm font-semibold">Elle monitor ekle</h2>
        <AddMonitorForm />
      </section>
    </main>
  );
}
