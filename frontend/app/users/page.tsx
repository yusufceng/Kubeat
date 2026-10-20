import { Suspense } from "react";
import {
  getLdapStatus,
  getOidcStatus,
  type GroupRole,
  type LdapStatus,
  listGroupRoles,
  listUsers,
  type OidcStatus,
  type User,
} from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT } from "@/lib/i18n/server";
import { workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { GroupRoleForm, GroupRoleRow, NewUserForm, UserRow } from "@/app/ui/user-forms";

async function Users() {
  const me = await currentUser();
  const t = await getT();
  if (!me.admin) {
    return <p className="card p-6 text-sm text-muted">{t("auth.adminsOnly")}</p>;
  }
  let users: User[];
  let ldap: LdapStatus;
  let oidc: OidcStatus;
  let groups: GroupRole[];
  let known: string[];
  try {
    const [u, l, o, g, monitors] = await Promise.all([listUsers(), getLdapStatus(), getOidcStatus(), listGroupRoles(), workspaceMonitors()]);
    users = u;
    ldap = l;
    oidc = o;
    groups = g;
    known = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  } catch (err) {
    return <BackendError what={t("nav.users")} err={err} />;
  }

  return (
    <>
      <ul className="card divide-y divide-line">
        {users.map((u) => (
          <UserRow key={u.id} user={u} self={u.id === me.id} known={known} />
        ))}
      </ul>
      <section className="card p-5">
        <h2 className="mb-1 text-sm font-semibold">{t("users.new.title")}</h2>
        <p className="mb-4 text-xs text-muted">{t("users.new.hint")}</p>
        <NewUserForm known={known} />
      </section>

      <section className="card p-5">
        <h2 className="mb-1 text-sm font-semibold">{t("users.oidc.title")}</h2>
        {oidc.enabled ? (
          <p className="text-xs text-muted">
            {oidc.name}: <span className="font-mono">{oidc.issuer}</span>. {t("users.oidc.enabled")}
            {oidc.default_role
              ? ` ${t("users.groups.defaultRole", { role: oidc.default_role })}`
              : ` ${t("users.groups.noDefaultRole")}`}
          </p>
        ) : oidc.problem ? (
          <p className="text-xs text-muted">
            {t("users.oidc.problem")} <code className="break-all font-mono">{oidc.problem}</code>
          </p>
        ) : (
          <p className="text-xs text-muted">
            {t("users.oidc.offBefore")} <code className="font-mono">UPTIME_OIDC_ISSUER</code>,{" "}
            <code className="font-mono">UPTIME_OIDC_CLIENT_ID</code> {t("users.oidc.offAnd")}{" "}
            <code className="font-mono">UPTIME_OIDC_REDIRECT_URL</code> {t("users.oidc.offAfter")}
          </p>
        )}
      </section>

      <section className="card p-5">
        <h2 className="mb-1 text-sm font-semibold">{t("users.ldap.title")}</h2>
        {ldap.enabled ? (
          <>
            <p className="mb-4 text-xs text-muted">
              {t("users.ldap.directory")} <span className="font-mono">{ldap.url}</span>. {t("users.ldap.enabled")}
              {ldap.default_role
                ? ` ${t("users.groups.defaultRole", { role: ldap.default_role })}`
                : ` ${t("users.groups.noDefaultRole")}`}
            </p>
            {groups.length > 0 && (
              <ul className="card mb-4 divide-y divide-line">
                {groups.map((g) => (
                  <GroupRoleRow key={g.id} group={g} />
                ))}
              </ul>
            )}
            <GroupRoleForm known={known} />
          </>
        ) : (
          <p className="text-xs text-muted">
            {t("users.ldap.offBefore")} <code className="font-mono">UPTIME_LDAP_URL</code> {t("users.ldap.offAfter")}
          </p>
        )}
      </section>
    </>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("nav.users")}</h1>
        <p className="mt-1 text-sm text-muted">{t("users.intro")}</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <Users />
      </Suspense>
    </main>
  );
}
