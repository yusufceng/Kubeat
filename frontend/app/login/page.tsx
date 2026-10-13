import { redirect } from "next/navigation";
import { Suspense } from "react";
import { getProviders, type Providers } from "@/lib/api";
import { LoginForm } from "@/app/login/login-form";

/** What went wrong on the way back from the identity provider. */
const oidcMessages: Record<string, string> = {
  norole: "Kimlik sağlayıcı sizi tanıdı ama burada bir rolünüz yok; bir yöneticiye başvurun.",
  unavailable: "Kimlik sağlayıcıya ulaşılamadı. Biraz sonra tekrar deneyin.",
  refused: "Kimlik sağlayıcı girişi tamamlamadı; tekrar deneyin.",
};

async function Form({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  let providers: Providers | null = null;
  try {
    providers = await getProviders();
  } catch {
    // The form still shows; signing in will say what is wrong.
  }
  // Nobody can sign in until the first account exists. (Outside the try:
  // redirect() works by throwing.)
  if (providers?.setup_needed) redirect("/setup");
  const ldap = providers?.ldap ?? false;
  const oidc = providers?.oidc ?? null;
  const next = params.next;
  const oidcProblem = typeof params.oidc === "string" ? oidcMessages[params.oidc] : undefined;
  return (
    <>
      {params.expired !== undefined && (
        <p role="status" className="rounded-lg bg-sunken px-3 py-2 text-sm text-muted">
          Oturumunuz sona erdi; yeniden giriş yapın.
        </p>
      )}
      {oidcProblem && (
        <p role="alert" className="rounded-lg border border-down/40 bg-down/8 px-3 py-2 text-sm text-down">
          {oidcProblem}
        </p>
      )}
      {oidc && (
        <>
          <a href="/auth/oidc" className="btn btn-primary justify-center">
            {oidc.name} ile giriş yap
          </a>
          <p className="text-center text-xs text-muted">ya da hesapla</p>
        </>
      )}
      <LoginForm next={typeof next === "string" ? next : "/"} ldap={ldap} />
    </>
  );
}

export default function Page({ searchParams }: PageProps<"/login">) {
  return (
    <main className="mx-auto flex min-h-full w-full max-w-sm flex-col justify-center px-4 py-16">
      <div className="card flex flex-col gap-6 p-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Giriş</h1>
          <p className="mt-1 text-sm text-muted">Devam etmek için hesabınızla giriş yapın.</p>
        </div>
        <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
          <Form searchParams={searchParams} />
        </Suspense>
      </div>
    </main>
  );
}
