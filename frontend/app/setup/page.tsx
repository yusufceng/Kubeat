import { redirect } from "next/navigation";
import { Suspense } from "react";
import { getProviders } from "@/lib/api";
import { getT } from "@/lib/i18n/server";
import { SetupForm } from "@/app/setup/setup-form";

async function Gate() {
  // Once an account exists this page has nothing to do.
  const providers = await getProviders();
  if (!providers.setup_needed) redirect("/login");
  return <SetupForm />;
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex min-h-full w-full max-w-sm flex-col justify-center px-4 py-16">
      <div className="card flex flex-col gap-6 p-6">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t("setup.title")}</h1>
          <p className="mt-1 text-sm text-muted">{t("setup.intro")}</p>
        </div>
        <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
          <Gate />
        </Suspense>
      </div>
    </main>
  );
}
