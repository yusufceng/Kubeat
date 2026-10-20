import Link from "next/link";
import { Suspense } from "react";
import { listMonitors, listStatusPages, type StatusPage } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT, titled } from "@/lib/i18n/server";
import { compareNamespaces } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { EmptyState, NoMonitors } from "@/app/ui/empty-state";
import { PlusIcon, StatusPagesIcon } from "@/app/ui/icons";
import { monitorLabel } from "@/app/ui/monitor-name";
import { PageHead } from "@/app/ui/page-head";
import { type PageMonitorOption, StatusPageForm, StatusPageRow } from "@/app/ui/status-page-forms";

type Search = PageProps<"/status-pages">["searchParams"];

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
      .map((m) => ({ id: m.id, label: monitorLabel(m).name, namespace: m.namespace }))
      .sort((a, b) => compareNamespaces(a.namespace, b.namespace) || a.label.localeCompare(b.label, t.intl));
  } catch (err) {
    return <BackendError what={t("nav.statusPages")} err={err} />;
  }
  // A page shows monitors: with none yet, the way to the first one comes first.
  if (pages.length === 0 && monitors.length === 0) return <NoMonitors canEdit={me.can_edit} />;
  const adding = me.can_edit && params.new !== undefined;
  const editing = me.can_edit ? Number(params.edit) || null : null;

  return (
    <>
      {adding && (
        <section className="panel pg-create" aria-label={t("statusPages.new.title")}>
          <StatusPageForm monitors={monitors} needsMonitor={!me.admin} closeHref="/status-pages" />
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
                  canEdit={me.can_edit}
                  editing={editing === p.id}
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
