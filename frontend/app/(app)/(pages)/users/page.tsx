import Link from "next/link";
import { Suspense } from "react";
import {
  getLdapStatus,
  getLicense,
  getOidcStatus,
  type GroupRole,
  type LdapStatus,
  type License,
  licenseFeatures,
  listGroupRoles,
  listUsers,
  type OidcStatus,
  type User,
} from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT, titled } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { EmptyState } from "@/app/ui/empty-state";
import { PlusIcon } from "@/app/ui/icons";
import { LockIcon } from "@/app/ui/page-icons";
import { PageHead } from "@/app/ui/page-head";
import { type GlyphKind, glyphTone, StatusGlyph } from "@/app/ui/status-glyph";
import { GroupRoleForm, GroupRoleRow, UserForm, UserRow } from "@/app/ui/user-forms";

type Search = PageProps<"/users">["searchParams"];

/**
 * Whether a way of signing in works: on, off, set up but refused by its own
 * settings, or set up and waiting for a license.
 */
function ProviderState({ kind, t }: { kind: "on" | "off" | "problem" | "license"; t: T }) {
  const glyph: GlyphKind = kind === "on" ? "up" : kind === "problem" ? "down" : kind === "license" ? "warn" : "paused";
  return (
    <span className="pg-state" style={{ color: glyphTone[glyph].ink }}>
      <StatusGlyph kind={glyph} />
      {t(`users.provider.${kind}`)}
    </span>
  );
}

/** A setting of the backend, as it is written. */
const Setting = ({ name }: { name: string }) => <code className="pg-mono text-ink">{name}</code>;

async function Users({ searchParams }: { searchParams: Search }) {
  const me = await currentUser();
  const t = await getT();
  if (!me.admin) {
    return <EmptyState icon={<LockIcon width={20} height={20} />} title={t("auth.adminsOnly")} />;
  }
  const params = await searchParams;
  let users: User[];
  let ldap: LdapStatus;
  let oidc: OidcStatus;
  let groups: GroupRole[];
  let known: string[];
  let license: License | null;
  try {
    const [u, l, o, g, monitors, lic] = await Promise.all([
      listUsers(),
      getLdapStatus(),
      getOidcStatus(),
      listGroupRoles(),
      workspaceMonitors(),
      getLicense().catch(() => null), // a backend from before licenses
    ]);
    users = u;
    ldap = l;
    oidc = o;
    groups = g;
    license = lic;
    known = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  } catch (err) {
    return <BackendError what={t("nav.users")} err={err} />;
  }
  const editing = Number(params.edit) || null;
  // Set up, but off for want of a license: the backend has the reason in English.
  const ssoUnlicensed = license !== null && !license.features.includes("sso");
  const oidcState = oidc.enabled ? "on" : oidc.issuer && ssoUnlicensed ? "license" : oidc.problem ? "problem" : "off";
  const ldapState = ldap.enabled ? "on" : ldap.url && ssoUnlicensed ? "license" : "off";
  const defaultRole = (role: string) =>
    role ? t("users.groups.defaultRole", { role: t(`auth.role.${role}`) }) : t("users.groups.noDefaultRole");

  return (
    <>
      {params.new !== undefined && (
        <section className="panel pg-create" aria-label={t("users.new.title")}>
          <UserForm known={known} closeHref="/users" />
        </section>
      )}

      <div className="panel pg-table-panel">
        <table className="pg-table">
          <thead>
            <tr>
              <th scope="col">{t("users.col.user")}</th>
              <th scope="col">{t("auth.role")}</th>
              <th scope="col">{t("users.col.scope")}</th>
              <th scope="col">{t("users.col.source")}</th>
              <th scope="col">{t("users.col.lastSignIn")}</th>
              <th scope="col" className="pg-actions">
                <span className="sr-only">{t("table.actions")}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <UserRow key={u.id} user={u} self={u.id === me.id} known={known} editing={editing === u.id} />
            ))}
          </tbody>
        </table>
      </div>

      <section aria-labelledby="providers" className="flex flex-col gap-4">
        <div className="pg-section-head mb-0">
          <h2 id="providers">{t("users.providers.title")}</h2>
          <p>{t("users.providers.body")}</p>
        </div>

        <div className="panel">
          <div className="pg-provider-head">
            <h3>{t("users.oidc.title")}</h3>
            <ProviderState kind={oidcState} t={t} />
          </div>
          <div className="pg-provider-body">
            {oidcState === "license" ? (
              <p>
                {oidc.name}: <span className="pg-mono">{oidc.issuer}</span>. {t("users.sso.unlicensed")}
              </p>
            ) : oidc.enabled ? (
              <p>
                {oidc.name}: <span className="pg-mono">{oidc.issuer}</span>. {t("users.oidc.enabled")} {defaultRole(oidc.default_role)}
              </p>
            ) : oidc.problem ? (
              <p>
                {t("users.oidc.problem")} <code className="pg-mono break-all text-down-ink">{oidc.problem}</code>
              </p>
            ) : (
              <p>
                {t("users.oidc.offBefore")} <Setting name="UPTIME_OIDC_ISSUER" />, <Setting name="UPTIME_OIDC_CLIENT_ID" />{" "}
                {t("users.oidc.offAnd")} <Setting name="UPTIME_OIDC_REDIRECT_URL" /> {t("users.oidc.offAfter")}
              </p>
            )}
          </div>
        </div>

        <div className="panel">
          <div className="pg-provider-head">
            <h3>{t("users.ldap.title")}</h3>
            <ProviderState kind={ldapState} t={t} />
          </div>
          <div className="pg-provider-body">
            {ldapState === "license" ? (
              <p>
                {t("users.ldap.directory")} <span className="pg-mono">{ldap.url}</span>. {t("users.sso.unlicensed")}
              </p>
            ) : ldap.enabled ? (
              <p>
                {t("users.ldap.directory")} <span className="pg-mono">{ldap.url}</span>. {t("users.ldap.enabled")} {defaultRole(ldap.default_role)}
              </p>
            ) : (
              <p>
                {t("users.ldap.offBefore")} <Setting name="UPTIME_LDAP_URL" /> {t("users.ldap.offAfter")}
              </p>
            )}
          </div>
          {ldap.enabled && (
            <>
              {groups.length > 0 && (
                <div className="border-t border-line">
                  <table className="pg-table">
                    <thead>
                      <tr>
                        <th scope="col">{t("users.groups.col.group")}</th>
                        <th scope="col">{t("auth.role")}</th>
                        <th scope="col">{t("users.col.scope")}</th>
                        <th scope="col" className="pg-actions">
                          <span className="sr-only">{t("table.actions")}</span>
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {groups.map((g) => (
                        <GroupRoleRow key={g.id} group={g} />
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <div className="border-t border-line">
                <GroupRoleForm known={known} />
              </div>
            </>
          )}
        </div>
      </section>

      {license && <LicensePanel license={license} t={t} />}
    </>
  );
}

const licenseTone = { community: "paused", active: "up", expiring: "warn", expired: "down", invalid: "down" } as const;

/** Where the Kubeat Enterprise license stands, and what it turns on. */
function LicensePanel({ license, t }: { license: License; t: T }) {
  const glyph: GlyphKind = licenseTone[license.state];
  return (
    <section id="license" aria-labelledby="license-title" className="flex flex-col gap-4">
      <div className="pg-section-head mb-0">
        <h2 id="license-title">{t("license.title")}</h2>
        <p>{t("license.body")}</p>
      </div>
      <div className="panel">
        <div className="pg-provider-head">
          <h3>{license.customer || t("license.none")}</h3>
          <span className="pg-state" style={{ color: glyphTone[glyph].ink }}>
            <StatusGlyph kind={glyph} />
            {t(`license.state.${license.state}`)}
          </span>
        </div>
        <div className="pg-provider-body flex flex-col gap-2">
          {license.state === "community" && (
            <p>
              {t("license.noneBefore")} <Setting name="license.existingSecret" />
              {t("license.noneAnd")}{" "}
              <Setting name="UPTIME_LICENSE_FILE" />
              {t("license.noneAfter")}
            </p>
          )}
          {license.state === "invalid" && (
            <p>
              {t("license.problem")} <code className="pg-mono break-all text-down-ink">{license.problem}</code>
            </p>
          )}
          {(license.state === "expired" || license.state === "invalid") && <p>{t("license.ended")}</p>}
          {license.id && (
            <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1">
              <dt className="text-ink-3">{t("license.id")}</dt>
              <dd className="pg-mono">{license.id}</dd>
              {license.expires_at && (
                <>
                  <dt className="text-ink-3">{t("license.expires")}</dt>
                  <dd className="pg-mono">{license.expires_at.slice(0, 10)}</dd>
                </>
              )}
            </dl>
          )}
          <div>
            <p className="text-ink-3">{t("license.features")}</p>
            <ul className="mt-1 flex flex-col gap-1">
              {licenseFeatures.map((f) => {
                const on = license.features.includes(f);
                return (
                  <li key={f} className="flex items-center gap-2">
                    <StatusGlyph kind={on ? "up" : "paused"} />
                    <span className={on ? "text-ink" : "text-ink-3"}>{t(`license.feature.${f}`)}</span>
                  </li>
                );
              })}
            </ul>
          </div>
          {license.state !== "community" && <p className="text-ink-3">{t("license.renew")}</p>}
        </div>
      </div>
    </section>
  );
}

/** The head's action: a new local account. */
async function NewUser() {
  const me = await currentUser();
  if (!me.admin) return null;
  const t = await getT();
  return (
    <Link href="/users?new" className="btn pg-head-new">
      <PlusIcon />
      {t("users.new.title")}
    </Link>
  );
}

export const generateMetadata = titled("nav.users");

export default async function Page({ searchParams }: PageProps<"/users">) {
  const t = await getT();
  return (
    <main className="page">
      <PageHead title={t("nav.users")} intro={t("users.intro")}>
        <Suspense>
          <NewUser />
        </Suspense>
      </PageHead>
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.loading")}</p>}>
        <Users searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
