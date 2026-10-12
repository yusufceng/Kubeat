import { Suspense } from "react";
import {
  ApiError,
  type Candidate,
  type DiscoveryStatus,
  getDiscoveryStatus,
  listCandidates,
  listNamespaces,
} from "@/lib/api";
import { BackendError } from "@/app/ui/backend-error";
import { CandidateForm } from "@/app/ui/candidate-form";

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-lg border border-dashed border-black/15 p-8 text-center text-sm text-zinc-500 dark:border-white/20">
      {children}
    </p>
  );
}

function ClusterError({ err }: { err: unknown }) {
  if (!(err instanceof ApiError)) return <BackendError what="Cluster içeriği" />;
  return (
    <p
      role="alert"
      className="rounded-lg border border-red-600/40 bg-red-600/5 p-4 text-sm text-red-700 dark:text-red-400"
    >
      Cluster&apos;dan okunamadı: <span className="font-mono text-xs">{err.message}</span>
    </p>
  );
}

async function Discovery({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const param = (await searchParams).namespace;
  const namespace = typeof param === "string" ? param : "";

  let status: DiscoveryStatus;
  let namespaces: string[] = [];
  let clusterError: unknown = null;
  try {
    status = await getDiscoveryStatus();
    if (status.enabled) namespaces = await listNamespaces();
  } catch (err) {
    status = { enabled: true, detail: "" };
    clusterError = err;
  }
  if (clusterError !== null) return <ClusterError err={clusterError} />;
  if (!status.enabled) {
    return (
      <Notice>
        Keşif kullanılamıyor: backend bir cluster&apos;a bağlı değil.
        <span className="mt-2 block font-mono text-xs">{status.detail}</span>
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
      <form method="get" className="flex flex-wrap items-end gap-3">
        <label>
          <span className="mb-1 block text-xs font-medium text-zinc-500">Namespace</span>
          <select
            name="namespace"
            defaultValue={namespace}
            className="min-w-64 rounded-md border border-black/15 bg-transparent px-3 py-2 text-sm dark:border-white/20"
          >
            <option value="" disabled>
              Seçin…
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
          className="rounded-md border border-black/15 px-4 py-2 text-sm font-medium hover:bg-black/5 dark:border-white/20 dark:hover:bg-white/10"
        >
          Tara
        </button>
      </form>

      {!namespace && <Notice>Taranacak namespace&apos;i seçin.</Notice>}
      {candidatesError !== null && <ClusterError err={candidatesError} />}
      {candidates?.length === 0 && (
        <Notice>
          <span className="font-medium">{namespace}</span> içinde izlenebilecek Service, Ingress ya
          da Route bulunamadı.
        </Notice>
      )}
      {candidates && candidates.length > 0 && (
        // Keyed so that selection state starts fresh for another namespace.
        <CandidateForm key={namespace} namespace={namespace} candidates={candidates} />
      )}
    </>
  );
}

export default function Page({ searchParams }: PageProps<"/discovery">) {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-6 px-4 py-10">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Keşif</h1>
        <p className="mt-1 text-sm text-zinc-500">
          Bir namespace&apos;teki Service, Ingress ve Route&apos;ları bulur. Bir Service&apos;in
          arkasındaki pod HTTP probe tanımlıyorsa aynı yol kontrol edilir; yoksa yalnızca porta
          bağlanılır.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-zinc-500">Cluster okunuyor…</p>}>
        <Discovery searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
