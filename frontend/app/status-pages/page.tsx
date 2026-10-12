import { Suspense } from "react";
import { listMonitors, listStatusPages, type StatusPage } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { compareNamespaces } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { monitorLabel } from "@/app/ui/monitor-name";
import { type PageMonitorOption, StatusPageForm, StatusPageRow } from "@/app/ui/status-page-forms";

async function StatusPages() {
  const me = await currentUser();
  let pages: StatusPage[];
  let monitors: PageMonitorOption[];
  try {
    const [list, all] = await Promise.all([listStatusPages(), listMonitors()]);
    pages = list;
    monitors = all
      .map((m) => ({ id: m.id, label: monitorLabel(m).name, namespace: m.namespace }))
      .sort((a, b) => compareNamespaces(a.namespace, b.namespace) || a.label.localeCompare(b.label, "tr"));
  } catch (err) {
    return <BackendError what="Durum sayfaları" err={err} />;
  }

  return (
    <>
      {pages.length === 0 ? (
        <p className="rounded-xl border border-dashed border-line p-8 text-center text-sm text-muted">
          Henüz durum sayfası yok. Bir sayfa, seçtiğiniz monitor&apos;ların durumunu giriş yapmadan
          görülebilen bir adreste yayınlar.
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
          <h2 className="mb-4 text-sm font-semibold">Yeni durum sayfası</h2>
          <StatusPageForm monitors={monitors} />
        </section>
      )}
    </>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Durum sayfaları</h1>
        <p className="mt-1 text-sm text-muted">
          Seçilen servislerin durumunu ve uptime geçmişini, giriş gerektirmeyen bir adreste gösterir:
          ekip dışındakiler, müşteriler ya da duvardaki ekran için.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <StatusPages />
      </Suspense>
    </main>
  );
}
