import { redirect } from "next/navigation";
import { Suspense } from "react";
import { getProviders } from "@/lib/api";
import { SetupForm } from "@/app/setup/setup-form";

async function Gate() {
  // Once an account exists this page has nothing to do.
  const providers = await getProviders();
  if (!providers.setup_needed) redirect("/login");
  return <SetupForm />;
}

export default function Page() {
  return (
    <main className="mx-auto flex min-h-full w-full max-w-sm flex-col justify-center px-4 py-16">
      <div className="card flex flex-col gap-6 p-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Kubeat&apos;e hoş geldiniz</h1>
          <p className="mt-1 text-sm text-muted">
            Henüz bir hesap yok. İlk yönetici hesabını oluşturun; diğer hesapları sonra bu hesapla
            açarsınız.
          </p>
        </div>
        <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
          <Gate />
        </Suspense>
      </div>
    </main>
  );
}
