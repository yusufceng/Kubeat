import { Fragment, Suspense } from "react";
import {
  ApiError,
  type Candidate,
  type DiscoveryStatus,
  getDiscoveryStatus,
  listCandidates,
  listNamespaces,
} from "@/lib/api";
import { BackendError } from "@/app/ui/backend-error";
import { config } from "@/lib/config";
import { LocalTime } from "@/app/ui/local-time";
import { currentUser } from "@/lib/auth";
import { CandidateForm } from "@/app/ui/candidate-form";
import { restoreSourceAction } from "@/app/actions";
import { getT } from "@/lib/i18n/server";
import { typeLabel } from "@/lib/monitor-types";

/** The message with the given nodes in place of its {name} placeholders, so markup can sit inside a sentence. */
function withNodes(text: string, nodes: Record<string, React.ReactNode>): React.ReactNode[] {
  return text.split(/(\{\w+\})/).map((part, i) => {
    const name = part.slice(1, -1);
    return part.startsWith("{") && name in nodes ? <Fragment key={i}>{nodes[name]}</Fragment> : part;
  });
}

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-muted">
      {children}
    </p>
  );
}

async function ClusterError({ err }: { err: unknown }) {
  const t = await getT();
  if (!(err instanceof ApiError)) return <BackendError what={t("discovery.what")} />;
  return (
    <p
      role="alert"
      className="rounded-lg border border-down/40 bg-down/8 p-4 text-sm text-down"
    >
      {t("discovery.clusterError")} <span className="font-mono text-xs">{err.message}</span>
    </p>
  );
}

/**
 * How the monitors asked for with annotations are doing. For a user who does
 * not see every namespace (scoped), the backend counts and lists only theirs.
 */
async function SyncCard({ sync, canEdit, scoped }: { sync: DiscoveryStatus["sync"]; canEdit: boolean; scoped: boolean }) {
  const t = await getT();
  const snippet = (text: string) => <code className="rounded bg-sunken px-1">{text}</code>;
  return (
    <section className="card p-5 text-sm" aria-label={t("discovery.sync.title")}>
      <h2 className="font-semibold">{t("discovery.sync.title")}</h2>
      <p className="mt-1 text-muted">
        {withNodes(t("discovery.sync.intro"), {
          monitor: snippet('kubeat.io/monitor: "true"'),
          interval: snippet("kubeat.io/interval"),
          timeout: snippet("kubeat.io/timeout"),
          threshold: snippet("kubeat.io/failure-threshold"),
          port: snippet("kubeat.io/port"),
          path: snippet("kubeat.io/path"),
        })}
      </p>
      {sync === null ? (
        <p className="mt-3 text-xs text-muted">
          {withNodes(t("discovery.sync.off"), { variable: <code>UPTIME_DISCOVERY_SYNC_INTERVAL</code> })}
        </p>
      ) : (
        <>
          <p className="mt-3 text-xs text-muted">
            {sync.last_run_at.startsWith("0001") ? (
              t("discovery.sync.notYet")
            ) : (
              <>
                {t("discovery.sync.lastScan")} <LocalTime iso={sync.last_run_at} /> ·{" "}
                {t("discovery.sync.every", { interval: t("format.seconds", { count: sync.interval_seconds }) })} ·{" "}
                {t.n("discovery.sync.managed", sync.managed)}
                {sync.created + sync.updated + sync.removed > 0 &&
                  ` · ${t("discovery.sync.changes", { created: sync.created, updated: sync.updated, removed: sync.removed })}`}
              </>
            )}
          </p>
          {scoped && <p className="mt-1 text-xs text-muted">{t("discovery.sync.scoped")}</p>}
          {sync.error && (
            <p role="alert" className="mt-2 text-xs text-down">
              {t("discovery.sync.readError")} <span className="font-mono">{sync.error}</span>
            </p>
          )}
          {sync.skipped.length > 0 && (
            <ul className="mt-2 space-y-1 text-xs">
              {sync.skipped.map((s) => (
                <li key={s.namespace + "/" + s.source}>
                  <span className="font-mono">
                    {s.namespace}/{s.source}
                  </span>
                  <span className="text-muted"> — {s.dismissed ? t("discovery.sync.dismissed") : s.reason}</span>
                  {s.dismissed && canEdit && (
                    <form action={restoreSourceAction} className="ml-2 inline">
                      <input type="hidden" name="namespace" value={s.namespace} />
                      <input type="hidden" name="source" value={s.source} />
                      <button type="submit" className="font-medium underline hover:text-ink">
                        {t("discovery.sync.restore")}
                      </button>
                    </form>
                  )}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </section>
  );
}

/** The candidates of a namespace for a viewer, who may not import them. */
async function ReadOnlyCandidates({ candidates }: { candidates: Candidate[] }) {
  const t = await getT();
  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-line text-xs text-muted">
          <tr>
            <th className="px-4 py-2 font-medium">{t("discovery.candidates.object")}</th>
            <th className="px-4 py-2 font-medium">{t("discovery.candidates.check")}</th>
            <th className="px-4 py-2 font-medium">{t("discovery.candidates.status")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {candidates.map((c) => (
            <tr key={c.source}>
              <td className="px-4 py-2">
                <span className="mr-2 text-xs text-muted">{c.kind}</span>
                {c.name}
                {c.port > 0 && `:${c.port}`}
                {c.no_endpoints && (
                  <span className="ml-2 rounded bg-warn/12 px-1.5 py-0.5 text-xs font-medium text-warn">
                    {t("discovery.import.noEndpoints")}
                  </span>
                )}
              </td>
              <td className="px-4 py-2">
                <span className="mr-2 text-xs text-muted">{t(typeLabel(c.type))}</span>
                <span className="break-all font-mono text-xs">{c.target}</span>
              </td>
              <td className="whitespace-nowrap px-4 py-2 text-xs text-muted">
                {c.monitor_id !== null ? t("discovery.candidates.monitored") : t("discovery.candidates.notMonitoredViewer")}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

async function Discovery({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const param = (await searchParams).namespace;
  const namespace = typeof param === "string" ? param : "";
  const me = await currentUser();
  const t = await getT();

  let status: DiscoveryStatus;
  let namespaces: string[] = [];
  let clusterError: unknown = null;
  try {
    status = await getDiscoveryStatus();
    if (status.enabled) namespaces = await listNamespaces();
  } catch (err) {
    status = { enabled: true, detail: "", sync: null };
    clusterError = err;
  }
  if (clusterError !== null) return <ClusterError err={clusterError} />;
  if (!status.enabled) {
    // Why there is no cluster is told to administrators only.
    return (
      <Notice>
        {t("discovery.unavailable")}
        {status.detail && <span className="mt-2 block font-mono text-xs">{status.detail}</span>}
      </Notice>
    );
  }

  let candidates: Candidate[] | null = null;
  let candidatesError: unknown = null;
  if (namespace) {
    try {
      candidates = await listCandidates(namespace);
    } catch (err) {
      candidatesError = err;
    }
  }

  return (
    <>
      <SyncCard sync={status.sync} canEdit={me.can_edit} scoped={!me.sees_all} />
      <form method="get" className="flex flex-wrap items-end gap-3">
        <label>
          <span className="mb-1 block text-xs font-medium text-muted">{t("discovery.namespace")}</span>
          <select
            name="namespace"
            defaultValue={namespace}
            className="field w-auto min-w-64"
          >
            <option value="" disabled>
              {t("discovery.namespace.choose")}
            </option>
            {namespaces.map((ns) => (
              <option key={ns} value={ns}>
                {ns}
              </option>
            ))}
          </select>
        </label>
        <button
          type="submit"
          className="btn"
        >
          {t("discovery.scan")}
        </button>
      </form>

      {!namespace && <Notice>{t("discovery.chooseNamespace")}</Notice>}
      {candidatesError !== null && <ClusterError err={candidatesError} />}
      {candidates?.length === 0 && (
        <Notice>
          {withNodes(t("discovery.candidates.none"), {
            namespace: <span className="font-medium">{namespace}</span>,
          })}
        </Notice>
      )}
      {candidates && candidates.length > 0 && (
        // Keyed so that selection state starts fresh for another namespace.
        me.can_edit ? (
          <CandidateForm key={namespace} namespace={namespace} candidates={candidates} defaults={config.newMonitor} />
        ) : (
          <ReadOnlyCandidates candidates={candidates} />
        )
      )}
    </>
  );
}

export default async function Page({ searchParams }: PageProps<"/discovery">) {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("nav.discovery")}</h1>
        <p className="mt-1 text-sm text-muted">{t("discovery.intro")}</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.readingCluster")}</p>}>
        <Discovery searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
