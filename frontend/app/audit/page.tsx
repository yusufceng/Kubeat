import { Suspense } from "react";
import { type AuditEntry, listAudit } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { config } from "@/lib/config";
import { BackendError } from "@/app/ui/backend-error";
import { LocalTime } from "@/app/ui/local-time";

const actionLabels: Record<string, string> = {
  create: "oluşturdu",
  update: "değiştirdi",
  delete: "sildi",
  login: "giriş yaptı",
  login_failed: "giriş yapamadı",
  login_denied: "giriş reddedildi",
  setup: "ilk hesabı oluşturdu",
  password_change: "parolasını değiştirdi",
};

const objectLabels: Record<string, string> = {
  monitor: "monitor",
  channel: "bildirim kanalı",
  maintenance: "bakım penceresi",
  status_page: "durum sayfası",
  user: "kullanıcı",
  ldap_group: "LDAP grup eşlemesi",
};

async function Audit() {
  const me = await currentUser();
  if (!me.admin) {
    return <p className="card p-6 text-sm text-muted">Bu sayfa yöneticilere açıktır.</p>;
  }
  let entries: AuditEntry[];
  try {
    entries = await listAudit(config.auditLimit);
  } catch (err) {
    return <BackendError what="Kayıt" err={err} />;
  }
  if (entries.length === 0) {
    return <p className="card p-6 text-center text-sm text-muted">Henüz kayıt yok.</p>;
  }
  return (
    <div className="card overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-line text-xs text-muted">
          <tr>
            <th className="px-4 py-2 font-medium">Zaman</th>
            <th className="px-4 py-2 font-medium">Kim</th>
            <th className="px-4 py-2 font-medium">Ne yaptı</th>
            <th className="px-4 py-2 font-medium">Ayrıntı</th>
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
                {e.object_type && `${objectLabels[e.object_type] ?? e.object_type} `}
                {actionLabels[e.action] ?? e.action}
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

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Kayıt</h1>
        <p className="mt-1 text-sm text-muted">Kim, ne zaman, neyi değiştirdi; girişler ve başarısız denemeler.</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <Audit />
      </Suspense>
    </main>
  );
}
