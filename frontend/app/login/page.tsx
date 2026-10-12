import { Suspense } from "react";
import { LoginForm } from "@/app/login/login-form";

async function Form({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const next = (await searchParams).next;
  return <LoginForm next={typeof next === "string" ? next : "/"} />;
}

export default function Page({ searchParams }: PageProps<"/login">) {
  return (
    <main className="mx-auto flex min-h-full w-full max-w-sm flex-col justify-center px-4 py-16">
      <div className="card flex flex-col gap-6 p-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Giriş</h1>
          <p className="mt-1 text-sm text-muted">Devam etmek için yönetici parolasını girin.</p>
        </div>
        <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
          <Form searchParams={searchParams} />
        </Suspense>
      </div>
    </main>
  );
}
