import { Suspense } from "react";
import { type AuditEntry, listAudit } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT, titled } from "@/lib/i18n/server";
import { BackendError } from "@/app/ui/backend-error";
import { EmptyState } from "@/app/ui/empty-state";
import { BellIcon, EditIcon, PlusIcon, TrashIcon } from "@/app/ui/icons";
import { LockIcon, LogIcon, SignInIcon, WarningIcon } from "@/app/ui/page-icons";
import { PageHead } from "@/app/ui/page-head";
import { TableTime } from "@/app/ui/table-time";

// Message keys; an action or kind the UI does not know is shown as the
// backend names it (t hands an unknown key back unchanged).
const actionLabels: Record<string, string> = {
  create: "audit.action.create",
  update: "audit.action.update",
  delete: "audit.action.delete",
  login: "audit.action.login",
  login_failed: "audit.action.loginFailed",
  login_denied: "audit.action.loginDenied",
  setup: "audit.action.setup",
  password_change: "audit.action.passwordChange",
  test: "audit.action.test",
};

const objectLabels: Record<string, string> = {
  monitor: "audit.object.monitor",
  channel: "audit.object.channel",
  maintenance: "audit.object.maintenance",
  status_page: "audit.object.statusPage",
  status_page_update: "audit.object.statusPageUpdate",
  user: "audit.object.user",
  ldap_group: "audit.object.ldapGroup",
  incident_note: "audit.object.incidentNote",
};

async function Audit() {
  const me = await currentUser();
  const t = await getT();
  if (!me.admin) {
    return <EmptyState icon={<LockIcon width={20} height={20} />} title={t("auth.adminsOnly")} />;
  }
  let entries: AuditEntry[];
  try {
    entries = await listAudit(config.auditLimit);
  } catch (err) {
    return <BackendError what={t("nav.audit")} err={err} />;
  }
  if (entries.length === 0) {
    return (
      <EmptyState icon={<LogIcon width={20} height={20} />} title={t("audit.empty")}>
        {t("audit.emptyBody")}
      </EmptyState>
    );
  }
  return (
    <section aria-label={t("nav.audit")}>
      <p className="pg-count">
        {entries.length >= config.auditLimit ? t("audit.newest", { count: entries.length }) : t.n("audit.count", entries.length)}
      </p>
      <div className="panel pg-table-panel">
        <table className="pg-table pg-table-inline">
          <thead>
            <tr>
              <th scope="col">{t("audit.col.time")}</th>
              <th scope="col">{t("audit.col.who")}</th>
              <th scope="col">{t("audit.col.what")}</th>
              <th scope="col">{t("audit.col.detail")}</th>
            </tr>
          </thead>
          <tbody>
            {entries.map((e) => {
              const look = actionLooks[e.action];
              const detail = `${e.summary}${e.object_id > 0 ? ` #${e.object_id}` : ""}`;
              return (
                <tr key={e.id}>
                  <td className="pg-time pg-shrink">
                    <TableTime iso={e.at} />
                  </td>
                  <td className="pg-shrink">
                    <span className="pg-trunc pg-name max-w-48" title={e.username}>
                      {e.username}
                    </span>
                  </td>
                  <td className="pg-shrink">
                    <span className={`pg-audit-what ${look?.tone ?? ""}`}>
                      {look?.icon ?? <EditIcon />}
                      <span>
                        {e.object_type && `${t(objectLabels[e.object_type] ?? e.object_type)} `}
                        {t(actionLabels[e.action] ?? e.action)}
                      </span>
                    </span>
                  </td>
                  <td className="pg-grow">
                    <span className="pg-trunc text-ink-2" title={detail}>
                      {e.summary}
                      {e.object_id > 0 && <span className="pg-mono ml-1.5 text-ink-3">#{e.object_id}</span>}
                    </span>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

/**
 * How each action is marked in its row: an icon, and for a sign-in that
 * failed or was refused the color of a warning, since those are what
 * someone reads the log for.
 */
const actionLooks: Record<string, { icon: React.ReactNode; tone?: string }> = {
  create: { icon: <PlusIcon /> },
  update: { icon: <EditIcon /> },
  delete: { icon: <TrashIcon /> },
  login: { icon: <SignInIcon /> },
  login_failed: { icon: <WarningIcon />, tone: "pg-audit-warn" },
  login_denied: { icon: <WarningIcon />, tone: "pg-audit-down" },
  setup: { icon: <PlusIcon /> },
  password_change: { icon: <LockIcon /> },
  test: { icon: <BellIcon /> },
};

export const generateMetadata = titled("nav.audit");

export default async function Page() {
  const t = await getT();
  return (
    <main className="page page-wide">
      <PageHead title={t("nav.audit")} intro={t("audit.intro")} />
      <Suspense fallback={<p className="text-sm text-ink-3">{t("common.loading")}</p>}>
        <Audit />
      </Suspense>
    </main>
  );
}
