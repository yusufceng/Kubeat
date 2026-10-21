import Link from "next/link";
import { Suspense } from "react";
import { type Incident, listIncidents, listMonitors, listStatusPages, listStatusPageUpdates, previewStatusPage, queryIncidents, type StatusPage } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT, titled } from "@/lib/i18n/server";
import { compareNamespaces } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { EmptyState, NoMonitors } from "@/app/ui/empty-state";
import { PlusIcon, StatusPagesIcon } from "@/app/ui/icons";
import { monitorLabel } from "@/app/ui/monitor-name";
import { PageHead } from "@/app/ui/page-head";
import { type PageExtras, type PageMonitorOption, StatusPageForm, StatusPageRow } from "@/app/ui/status-page-forms";
import type { IncidentOption } from "@/app/ui/status-page-updates";

type Search = PageProps<"/status-pages">["searchParams"];

/**
 * What the editor of a page sees besides its form: what the page shows now,
 * the updates it has published, and the recent incidents of the monitors
 * it shows, for an update to point at. Each part fails on its own: the
 * form is still there without them.
 */
async function readExtras(page: StatusPage, names: Map<number, string>): Promise<PageExtras> {
  // The outages still going on, whenever they began, and the newest ones:
  // a flapping monitor must not push a long outage off the list.
  const [preview, updates, open, recent] = await Promise.all([
    previewStatusPage(page.id).catch(() => null),
    listStatusPageUpdates(page.id, config.statusPageUpdatesLimit).catch(() => null),
    queryIncidents({ kind: "down", state: "open", limit: config.incidentsLimit }).catch(() => [] as Incident[]),
    listIncidents(config.incidentsLimit).catch(() => [] as Incident[]),
  ]);
  const incidents = [...open, ...recent.filter((i) => !open.some((o) => o.id === i.id))];
  // The monitors the page shows: by name, and by rule as of now.
  const shown = new Set<number>(page.monitors.map((m) => m.monitor_id));
  const pageNames = new Map<number, string>();
  for (const s of preview?.sections ?? []) {
    for (const m of s.monitors) {
      shown.add(m.monitor_id);
      pageNames.set(m.monitor_id, m.name);
    }
  }
  const options: IncidentOption[] = incidents
    .filter((i) => i.kind === "down" && shown.has(i.monitor_id))
    .map((i) => ({
      id: i.id,
      monitor: pageNames.get(i.monitor_id) ?? names.get(i.monitor_id) ?? i.monitor_name,
      started_at: i.started_at,
      resolved_at: i.resolved_at,
    }));
  return { preview, updates, incidents: options };
}

async function StatusPages({ searchParams }: { searchParams: Search }) {
  const t = await getT();
  const me = await currentUser();
  const params = await searchParams;
  let pages: StatusPage[];
  let monitors: PageMonitorOption[];
  try {
    const [list, all] = await Promise.all([listStatusPages(), listMonitors()]);
    pages = list;
    monitors = all
      .map((m) => ({ id: m.id, label: monitorLabel(m).name, namespace: m.namespace, tags: m.tags }))
      .sort((a, b) => compareNamespaces(a.namespace, b.namespace) || a.label.localeCompare(b.label, t.intl));
  } catch (err) {
    return <BackendError what={t("nav.statusPages")} err={err} />;
  }
  // A page shows monitors: with none yet, the way to the first one comes first.
  if (pages.length === 0 && monitors.length === 0) return <NoMonitors canEdit={me.can_edit} />;
  const adding = me.can_edit && params.new !== undefined;
  const editing = me.can_edit ? Number(params.edit) || null : null;
  const edited = pages.find((p) => p.id === editing);
  const extras = edited ? await readExtras(edited, new Map(monitors.map((m) => [m.id, m.label]))) : undefined;
  // A rule for every namespace is for those who edit every namespace.
  const anyNamespace = me.admin || me.sees_all;

  return (
    <>
      {adding && (
        <section className="panel pg-create" aria-label={t("statusPages.new.title")}>
          <StatusPageForm monitors={monitors} needsMonitor={!me.admin} anyNamespace={anyNamespace} closeHref="/status-pages" />
        </section>
      )}
      {pages.length === 0 ? (
        !adding && (
          <EmptyState
            icon={<StatusPagesIcon width={20} height={20} />}
            title={t("statusPages.empty.title")}
            action={
              me.can_edit && (
                <Link href="/status-pages?new" className="btn btn-primary">
                  <PlusIcon />
                  {t("statusPages.new.title")}
                </Link>
              )
            }
          >
            {t("statusPages.empty")}
          </EmptyState>
        )
      ) : (
        <div className="panel pg-table-panel">
          <table className="pg-table">
            <thead>
              <tr>
                <th scope="col">{t("statusPages.col.page")}</th>
                <th scope="col">{t("statusPages.col.address")}</th>
                <th scope="col" className="pg-num">
                  {t("statusPages.col.services")}
                </th>
                <th scope="col">{t("statusPages.col.state")}</th>
                <th scope="col" className="pg-actions">
                  <span className="sr-only">{t("table.actions")}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {pages.map((p) => (
                <StatusPageRow
                  key={p.id}
                  page={p}
                  monitors={monitors}
                  needsMonitor={!me.admin}
                  anyNamespace={anyNamespace}
                  canEdit={me.can_edit}
                  editing={editing === p.id}
                  extras={editing === p.id ? extras : undefined}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}

/** The head's action: a new page, for those who may make one. */
async function NewPage() {
  const me = await currentUser();
  if (!me.can_edit) return null;
  const t = await getT();
  return (
    <Link href="/status-pages?new" className="btn pg-head-new">
      <PlusIcon />
      {t("statusPages.new.title")}
    </Link>
  );
}

export const generateMetadata = titled("nav.statusPages");

export default async function Page({ searchParams }: PageProps<"/status-pages">) {
  const t = await getT();
  return (
    <main className="page">
      <PageHead title={t("nav.statusPages")} intro={t("statusPages.intro")}>
        <Suspense>
          <NewPage />
        </Suspense>
      </PageHead>
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.loading")}</p>}>
        <StatusPages searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
