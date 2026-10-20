import { redirect } from "next/navigation";
import { Suspense } from "react";
import { getProviders, type Providers } from "@/lib/api";
import { getT } from "@/lib/i18n/server";
import { LoginForm } from "@/app/login/login-form";
import { AuthFrame } from "@/app/ui/auth-frame";
import { SignInIcon, WarningIcon } from "@/app/ui/page-icons";

/** What went wrong on the way back from the identity provider. */
const oidcMessages: Record<string, string> = {
  norole: "login.oidc.norole",
  unavailable: "login.oidc.unavailable",
  refused: "login.oidc.refused",
  state: "login.oidc.state",
};

async function Form({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  const t = await getT();
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
        <p role="status" className="pg-callout">
          <span className="pg-callout-body pt-0">{t("login.expired")}</span>
        </p>
      )}
      {oidcProblem && (
        <p role="alert" className="pg-callout pg-callout-down">
          <span className="pg-callout-icon" aria-hidden>
            <WarningIcon />
          </span>
          <span className="pg-callout-body">{t(oidcProblem)}</span>
        </p>
      )}
      <div className="panel auth-panel">
        {oidc && (
          <>
            <a href="/auth/oidc" className="btn w-full">
              <SignInIcon />
              {t("login.oidc.signIn", { name: oidc.name })}
            </a>
            <p className="auth-or">
              <span>{t("login.orAccount")}</span>
            </p>
          </>
        )}
        <LoginForm next={typeof next === "string" ? next : "/"} ldap={ldap} />
      </div>
    </>
  );
}

export default async function Page({ searchParams }: PageProps<"/login">) {
  const t = await getT();
  return (
    <AuthFrame title={t("login.title")}>
      <Suspense fallback={<div className="panel auth-panel h-[232px]" aria-busy="true" aria-label={t("common.loading")} />}>
        <Form searchParams={searchParams} />
      </Suspense>
    </AuthFrame>
  );
}
