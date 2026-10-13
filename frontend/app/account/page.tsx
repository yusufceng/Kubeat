import { Suspense } from "react";
import { currentUser } from "@/lib/auth";
import { getT } from "@/lib/i18n/server";
import { PasswordForm } from "@/app/ui/user-forms";

const sourceLabels = { local: "account.source.local", ldap: "account.source.ldap", oidc: "account.source.oidc" } as const;

async function Account() {
  const me = await currentUser();
  const t = await getT();
  return (
    <>
      <dl className="card grid gap-x-6 gap-y-2 p-5 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-xs text-muted">{t("auth.username")}</dt>
          <dd className="font-mono">{me.username}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">{t("auth.role")}</dt>
          <dd>
            {/* The role map of user-forms lives in a client module, so its
                entries cannot be read here; the key is built directly. */}
            {t(`auth.role.${me.role}`)}
            {!me.admin && ` · ${me.namespaces.join(", ")}`}
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted">{t("account.source")}</dt>
          <dd>{t(sourceLabels[me.source])}</dd>
        </div>
        {me.email && (
          <div>
            <dt className="text-xs text-muted">{t("auth.email")}</dt>
            <dd>{me.email}</dd>
          </div>
        )}
      </dl>
      <section className="card p-5">
        <h2 className="mb-4 text-sm font-semibold">{t("auth.password")}</h2>
        {me.source !== "local" ? (
          <p className="text-sm text-muted">{t("account.passwordElsewhere", { source: t(sourceLabels[me.source]) })}</p>
        ) : (
          <PasswordForm />
        )}
      </section>
    </>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <h1 className="text-2xl font-semibold tracking-tight">{t("account.title")}</h1>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <Account />
      </Suspense>
    </main>
  );
}
