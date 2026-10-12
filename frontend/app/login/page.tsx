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
    <main className="mx-auto flex w-full max-w-sm flex-1 flex-col justify-center gap-6 px-4 py-16">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Giriş</h1>
        <p className="mt-1 text-sm text-zinc-500">Devam etmek için yönetici parolasını girin.</p>
      </div>
      <Suspense fallback={<p className="text-sm text-zinc-500">Yükleniyor…</p>}>
        <Form searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
