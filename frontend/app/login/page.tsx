import { redirect } from "next/navigation";
import { Suspense } from "react";
import { getProviders, type Providers } from "@/lib/api";
import { LoginForm } from "@/app/login/login-form";

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
  const next = params.next;
  return (
    <>
      {params.expired !== undefined && (
        <p role="status" className="rounded-lg bg-sunken px-3 py-2 text-sm text-muted">
          Oturumunuz sona erdi; yeniden giriş yapın.
        </p>
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
