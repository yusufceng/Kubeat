import { Suspense } from "react";
import { listMonitors, listStatusPages, type StatusPage } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT } from "@/lib/i18n/server";
import { compareNamespaces } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { monitorLabel } from "@/app/ui/monitor-name";
import { type PageMonitorOption, StatusPageForm, StatusPageRow } from "@/app/ui/status-page-forms";

async function StatusPages() {
  const t = await getT();
  const me = await currentUser();
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

  return (
    <>
      {pages.length === 0 ? (
        <p className="rounded-xl border border-dashed border-line p-8 text-center text-sm text-muted">
          {t("statusPages.empty")}
        </p>
      ) : (
        <ul className="card divide-y divide-line">
          {pages.map((p) => (
            <StatusPageRow key={p.id} page={p} monitors={monitors} />
          ))}
        </ul>
      )}
      {me.can_edit && (
        <section className="card p-5">
          <h2 className="mb-4 text-sm font-semibold">{t("statusPages.new.title")}</h2>
          <StatusPageForm monitors={monitors} />
        </section>
      )}
    </>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("nav.statusPages")}</h1>
        <p className="mt-1 text-sm text-muted">{t("statusPages.intro")}</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <StatusPages />
      </Suspense>
    </main>
  );
}
