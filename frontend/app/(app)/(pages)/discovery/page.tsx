import Link from "next/link";
import { Fragment, Suspense } from "react";
import {
  ApiError,
  type Candidate,
  type DiscoveryStatus,
  getDiscoveryStatus,
  listCandidates,
  listNamespaces,
} from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { duration } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { restoreSourceAction } from "@/app/actions";
import { BackendError } from "@/app/ui/backend-error";
import { CandidateForm } from "@/app/ui/candidate-form";
import { CopyLine } from "@/app/ui/copy-line";
import { EmptyState, MONITOR_ANNOTATION } from "@/app/ui/empty-state";
import { DiscoveryIcon, PlusIcon } from "@/app/ui/icons";
import { UnplugIcon, WarningIcon } from "@/app/ui/page-icons";
import { PageHead } from "@/app/ui/page-head";
import { type GlyphKind, glyphTone, StatusGlyph } from "@/app/ui/status-glyph";
import { TableTime } from "@/app/ui/table-time";

/** The annotations that tune a monitor asked for by annotation, beside the one that asks for it. */
const OPTIONAL_ANNOTATIONS = ["kubeat.io/interval", "kubeat.io/timeout", "kubeat.io/failure-threshold", "kubeat.io/port", "kubeat.io/path"];

/** The message with the given nodes in place of its {name} placeholders, so markup can sit inside a sentence. */
function withNodes(text: string, nodes: Record<string, React.ReactNode>): React.ReactNode[] {
  return text.split(/(\{\w+\})/).map((part, i) => {
    const name = part.slice(1, -1);
    return part.startsWith("{") && name in nodes ? <Fragment key={i}>{nodes[name]}</Fragment> : part;
  });
}

/** Code inside a sentence. */
const Code = ({ children }: { children: string }) => <code className="pg-code-inline">{children}</code>;

async function ClusterError({ err }: { err: unknown }) {
  const t = await getT();
  if (!(err instanceof ApiError)) return <BackendError what={t("discovery.what")} />;
  return (
    <p role="alert" className="pg-callout pg-callout-down">
      <span className="pg-callout-icon" aria-hidden>
        <WarningIcon />
      </span>
      <span className="pg-callout-body">
        <span className="pg-callout-title">{t("discovery.clusterError")}</span>
        <span className="pg-mono break-all">{err.message}</span>
      </span>
    </p>
  );
}

/** The choice of a namespace to scan: a plain GET form, so a scan has an address of its own. */
function ScanForm({ namespaces, namespace, t }: { namespaces: string[]; namespace: string; t: T }) {
  return (
    <form method="get" className="pg-scan" role="search" aria-label={t("discovery.scanLabel")}>
      <label className="min-w-0 flex-1">
        <span className="sr-only">{t("discovery.namespace")}</span>
        <select name="namespace" defaultValue={namespace} required className="field pg-mono">
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
      <button type="submit" className={`btn ${namespace ? "" : "btn-primary"}`}>
        <DiscoveryIcon />
        {t("discovery.scan")}
      </button>
    </form>
  );
}

/**
 * How the monitors asked for with annotations are doing: whether the sync
 * runs, the line to put into a manifest, and the objects it passed over.
 * For a user who does not see every namespace (scoped), the backend counts
 * and lists only theirs.
 */
async function SyncPanel({ sync, canEdit, scoped }: { sync: DiscoveryStatus["sync"]; canEdit: boolean; scoped: boolean }) {
  const t = await getT();
  const state: { glyph: GlyphKind; label: string } =
    sync === null
      ? { glyph: "paused", label: "discovery.sync.stateOff" }
      : sync.error
        ? { glyph: "down", label: "discovery.sync.stateError" }
        : sync.last_run_at.startsWith("0001")
          ? { glyph: "pending", label: "discovery.sync.stateWaiting" }
          : { glyph: "up", label: "discovery.sync.stateOn" };
  return (
    <section className="panel" aria-labelledby="sync-title">
      <div className="pg-provider-head">
        <h2 id="sync-title" className="type-heading">
          {t("discovery.sync.title")}
        </h2>
        <span className="pg-state" style={{ color: glyphTone[state.glyph].ink }}>
          <StatusGlyph kind={state.glyph} />
          {t(state.label)}
        </span>
      </div>
      <div className="pg-provider-body flex flex-col gap-3">
        <p>
          {withNodes(t("discovery.sync.lead"), {
            monitor: <Code>{MONITOR_ANNOTATION}</Code>,
          })}
        </p>
        <div className="max-w-md">
          <CopyLine text={MONITOR_ANNOTATION} />
        </div>
        <p>
          {t("discovery.sync.optional")}{" "}
          {OPTIONAL_ANNOTATIONS.map((a, i) => (
            <Fragment key={a}>
              {i > 0 && " "}
              <Code>{a}</Code>
            </Fragment>
          ))}
        </p>
        {sync === null ? (
          <p className="text-ink-3">{withNodes(t("discovery.sync.off"), { variable: <Code>UPTIME_DISCOVERY_SYNC_INTERVAL</Code> })}</p>
        ) : (
          <p className="text-ink-3">
            {sync.last_run_at.startsWith("0001") ? (
              t("discovery.sync.notYet")
            ) : (
              <>
                {t("discovery.sync.lastScan")} <TableTime iso={sync.last_run_at} /> ·{" "}
                {t("discovery.sync.every", { interval: duration(sync.interval_seconds, t) })} ·{" "}
                {t.n("discovery.sync.managed", sync.managed)}
                {sync.created + sync.updated + sync.removed > 0 &&
                  ` · ${t("discovery.sync.changes", { created: sync.created, updated: sync.updated, removed: sync.removed })}`}
              </>
            )}
            {scoped && <span className="block">{t("discovery.sync.scoped")}</span>}
          </p>
        )}
        {sync?.error && (
          <p role="alert" className="pg-callout pg-callout-down">
            <span className="pg-callout-icon" aria-hidden>
              <WarningIcon />
            </span>
            <span className="pg-callout-body">
              <span className="pg-callout-title">{t("discovery.sync.readError")}</span>
              <span className="pg-mono break-all">{sync.error}</span>
            </span>
          </p>
        )}
      </div>
      {sync && sync.skipped.length > 0 && (
        <div className="border-t border-line">
          <h3 id="sync-skipped" className="pg-caption">
            {t("discovery.sync.skippedTitle")}
          </h3>
          <table className="pg-table" aria-labelledby="sync-skipped">
            <thead>
              <tr>
                <th scope="col">{t("discovery.candidates.object")}</th>
                <th scope="col">{t("discovery.sync.reason")}</th>
                {canEdit && (
                  <th scope="col" className="pg-actions">
                    <span className="sr-only">{t("table.actions")}</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {sync.skipped.map((s) => (
                <tr key={s.namespace + "/" + s.source}>
                  <td className="pg-shrink">
                    <span className="pg-trunc pg-mono max-w-80" title={`${s.namespace}/${s.source}`}>
                      {s.namespace}/{s.source}
                    </span>
                  </td>
                  <td className="pg-grow">
                    <span className="pg-trunc text-ink-2" title={s.dismissed ? t("discovery.sync.dismissed") : s.reason}>
                      {s.dismissed ? t("discovery.sync.dismissed") : s.reason}
                    </span>
                  </td>
                  {canEdit && (
                    <td className="pg-actions">
                      {s.dismissed && (
                        <form action={restoreSourceAction}>
                          <input type="hidden" name="namespace" value={s.namespace} />
                          <input type="hidden" name="source" value={s.source} />
                          <button type="submit" className="btn btn-ghost btn-sm">
                            {t("discovery.sync.restore")}
                          </button>
                        </form>
                      )}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

async function Discovery({ searchParams }: { searchParams: PageProps<"/discovery">["searchParams"] }) {
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
      <EmptyState
        icon={<UnplugIcon width={20} height={20} />}
        title={t("discovery.unavailableTitle")}
        action={
          me.can_edit && (
            <Link href="/monitors/new" className="btn">
              <PlusIcon />
              {t("noMonitors.add")}
            </Link>
          )
        }
      >
        <p>{t("discovery.unavailable")}</p>
        {status.detail && <p className="pg-mono mt-2 text-ink-3">{status.detail}</p>}
      </EmptyState>
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
  const monitored = candidates?.filter((c) => c.monitor_id !== null).length ?? 0;

  return (
    <>
      {!namespace ? (
        <EmptyState
          icon={<DiscoveryIcon width={20} height={20} />}
          title={t("discovery.choose.title")}
          action={<ScanForm namespaces={namespaces} namespace="" t={t} />}
        >
          {t("discovery.chooseNamespace")}
        </EmptyState>
      ) : (
        <section aria-labelledby="results-title" className="flex flex-col gap-3">
          <div className="pg-results-head">
            <h2 id="results-title" className="type-heading">
              {withNodes(t("discovery.results.title"), { namespace: <span className="pg-mono">{namespace}</span> })}
            </h2>
            {candidates && candidates.length > 0 && (
              <p className="pg-count mb-0">
                {t.n("discovery.results.count", candidates.length)}
                {monitored > 0 && ` · ${t.n("discovery.results.monitored", monitored)}`}
              </p>
            )}
            <div className="ml-auto w-full sm:w-auto">
              <ScanForm namespaces={namespaces} namespace={namespace} t={t} />
            </div>
          </div>
          {candidatesError !== null && <ClusterError err={candidatesError} />}
          {candidates?.length === 0 && (
            <EmptyState
              icon={<DiscoveryIcon width={20} height={20} />}
              title={t("discovery.candidates.noneTitle", { namespace })}
              action={
                me.can_edit && (
                  <Link href="/monitors/new" className="btn">
                    <PlusIcon />
                    {t("noMonitors.add")}
                  </Link>
                )
              }
            >
              {t("discovery.candidates.noneBody")}
            </EmptyState>
          )}
          {candidates && candidates.length > 0 && (
            // Keyed so that selection state starts fresh for another namespace.
            <CandidateForm
              key={namespace}
              namespace={namespace}
              candidates={candidates}
              defaults={config.newMonitor}
              canEdit={me.can_edit}
            />
          )}
        </section>
      )}
      <SyncPanel sync={status.sync} canEdit={me.can_edit} scoped={!me.sees_all} />
    </>
  );
}

export default async function Page({ searchParams }: PageProps<"/discovery">) {
  const t = await getT();
  return (
    <main className="page">
      <PageHead title={t("nav.discovery")} intro={t("discovery.intro")} />
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.readingCluster")}</p>}>
        <Discovery searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
