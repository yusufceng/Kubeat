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
    <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-muted">
      {children}
    </p>
  );
}

function ClusterError({ err }: { err: unknown }) {
  if (!(err instanceof ApiError)) return <BackendError what="Cluster içeriği" />;
  return (
    <p
      role="alert"
      className="rounded-lg border border-down/40 bg-down/8 p-4 text-sm text-down"
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
          <span className="mb-1 block text-xs font-medium text-muted">Namespace</span>
          <select
            name="namespace"
            defaultValue={namespace}
            className="field w-auto min-w-64"
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
          className="btn"
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
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Keşif</h1>
        <p className="mt-1 text-sm text-muted">
          Bir namespace&apos;teki Service, Ingress ve Route&apos;ları bulur. Bir Service&apos;in
          arkasındaki pod HTTP probe tanımlıyorsa aynı yol kontrol edilir; yoksa yalnızca porta
          bağlanılır.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">Cluster okunuyor…</p>}>
        <Discovery searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
