import { redirect } from "next/navigation";
import { Suspense } from "react";
import { getProviders } from "@/lib/api";
import { getT } from "@/lib/i18n/server";
import { SetupForm } from "@/app/setup/setup-form";
import { AuthFrame } from "@/app/ui/auth-frame";

async function Gate() {
  // Once an account exists this page has nothing to do.
  const providers = await getProviders();
  if (!providers.setup_needed) redirect("/login");
  return <SetupForm />;
}

export default async function Page() {
  const t = await getT();
  return (
    <AuthFrame title={t("setup.title")} intro={t("setup.intro")}>
      <Suspense fallback={<div className="panel auth-panel h-[360px]" aria-busy="true" aria-label={t("common.loading")} />}>
        <Gate />
      </Suspense>
      <p className="auth-next">{t("setup.next")}</p>
    </AuthFrame>
  );
}
