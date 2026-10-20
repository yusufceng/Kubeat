import { Suspense } from "react";
import { currentUser } from "@/lib/auth";
import { getT } from "@/lib/i18n/server";
import { LockIcon } from "@/app/ui/page-icons";
import { PageHead } from "@/app/ui/page-head";
import { PasswordForm } from "@/app/ui/user-forms";

const sourceLabels = { local: "account.source.local", ldap: "account.source.ldap", oidc: "account.source.oidc" } as const;

async function Account() {
  const me = await currentUser();
  const t = await getT();
  const scope = me.admin
    ? t("users.row.everything")
    : me.namespaces.includes("*")
      ? t("users.row.allNamespaces")
      : me.namespaces.join(", ") || t("common.none");
  return (
    <>
      <section className="panel px-5 py-2" aria-label={t("account.title")}>
        <dl className="pg-dl">
          <dt>{t("auth.username")}</dt>
          <dd className="pg-mono">{me.username}</dd>
          {me.display_name && (
            <>
              <dt>{t("auth.displayName")}</dt>
              <dd>{me.display_name}</dd>
            </>
          )}
          <dt>{t("auth.role")}</dt>
          <dd>
            {/* The role map of user-forms lives in a client module, so its
                entries cannot be read here; the key is built directly. */}
            {t(`auth.role.${me.role}`)}
            <span className="text-ink-3"> · {t(`users.roleHint.${me.role}`)}</span>
          </dd>
          <dt>{t("users.col.scope")}</dt>
          <dd className={me.admin ? "" : "pg-mono"}>{scope}</dd>
          <dt>{t("account.source")}</dt>
          <dd>{t(sourceLabels[me.source])}</dd>
          {me.email && (
            <>
              <dt>{t("auth.email")}</dt>
              <dd>{me.email}</dd>
            </>
          )}
        </dl>
      </section>
      {me.source !== "local" ? (
        <p className="pg-callout">
          <span className="pg-callout-icon" aria-hidden>
            <LockIcon />
          </span>
          <span className="pg-callout-body">
            <span className="pg-callout-title">{t("auth.password")}</span>
            {t("account.passwordElsewhere", { source: t(sourceLabels[me.source]) })}
          </span>
        </p>
      ) : (
        <section className="panel" aria-label={t("auth.password")}>
          <PasswordForm />
        </section>
      )}
    </>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="page">
      <PageHead title={t("account.title")} intro={t("account.intro")} />
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.loading")}</p>}>
        <Account />
      </Suspense>
    </main>
  );
}
