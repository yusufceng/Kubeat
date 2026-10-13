import { Suspense } from "react";
import { type AuditEntry, listAudit } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { getT } from "@/lib/i18n/server";
import { BackendError } from "@/app/ui/backend-error";
import { LocalTime } from "@/app/ui/local-time";

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
};

const objectLabels: Record<string, string> = {
  monitor: "audit.object.monitor",
  channel: "audit.object.channel",
  maintenance: "audit.object.maintenance",
  status_page: "audit.object.statusPage",
  user: "audit.object.user",
  ldap_group: "audit.object.ldapGroup",
};

async function Audit() {
  const me = await currentUser();
  const t = await getT();
  if (!me.admin) {
    return <p className="card p-6 text-sm text-muted">{t("auth.adminsOnly")}</p>;
  }
  let entries: AuditEntry[];
  try {
    entries = await listAudit(config.auditLimit);
  } catch (err) {
    return <BackendError what={t("nav.audit")} err={err} />;
  }
  if (entries.length === 0) {
    return <p className="card p-6 text-center text-sm text-muted">{t("audit.empty")}</p>;
  }
  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-line text-xs text-muted">
          <tr>
            <th className="px-4 py-2 font-medium">{t("audit.col.time")}</th>
            <th className="px-4 py-2 font-medium">{t("audit.col.who")}</th>
            <th className="px-4 py-2 font-medium">{t("audit.col.what")}</th>
            <th className="px-4 py-2 font-medium">{t("audit.col.detail")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {entries.map((e) => (
            <tr key={e.id}>
              <td className="whitespace-nowrap px-4 py-2 tabular-nums">
                <LocalTime iso={e.at} />
              </td>
              <td className="whitespace-nowrap px-4 py-2 font-medium">{e.username}</td>
              <td className="whitespace-nowrap px-4 py-2">
                {e.object_type && `${t(objectLabels[e.object_type] ?? e.object_type)} `}
                {t(actionLabels[e.action] ?? e.action)}
              </td>
              <td className="px-4 py-2 text-xs text-muted">
                {e.summary}
                {e.object_id > 0 && <span className="ml-1 font-mono">#{e.object_id}</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default async function Page() {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("nav.audit")}</h1>
        <p className="mt-1 text-sm text-muted">{t("audit.intro")}</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <Audit />
      </Suspense>
    </main>
  );
}
