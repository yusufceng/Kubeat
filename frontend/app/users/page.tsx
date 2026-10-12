import { Suspense } from "react";
import { getLdapStatus, type GroupRole, type LdapStatus, listGroupRoles, listUsers, type User } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { GroupRoleForm, GroupRoleRow, NewUserForm, UserRow } from "@/app/ui/user-forms";

async function Users() {
  const me = await currentUser();
  if (!me.admin) {
    return <p className="card p-6 text-sm text-muted">Bu sayfa yöneticilere açıktır.</p>;
  }
  let users: User[];
  let ldap: LdapStatus;
  let groups: GroupRole[];
  let known: string[];
  try {
    const [u, l, g, monitors] = await Promise.all([listUsers(), getLdapStatus(), listGroupRoles(), workspaceMonitors()]);
    users = u;
    ldap = l;
    groups = g;
    known = [...new Set(monitors.map((m) => m.namespace).filter(Boolean))].sort();
  } catch (err) {
    return <BackendError what="Kullanıcılar" err={err} />;
  }

  return (
    <>
      <ul className="card divide-y divide-line">
        {users.map((u) => (
          <UserRow key={u.id} user={u} self={u.id === me.id} known={known} />
        ))}
      </ul>
      <section className="card p-5">
        <h2 className="mb-1 text-sm font-semibold">Yeni kullanıcı</h2>
        <p className="mb-4 text-xs text-muted">Yerel hesap: parolası burada tutulur.</p>
        <NewUserForm known={known} />
      </section>

      <section className="card p-5">
        <h2 className="mb-1 text-sm font-semibold">LDAP</h2>
        {ldap.enabled ? (
          <>
            <p className="mb-4 text-xs text-muted">
              Dizin: <span className="font-mono">{ldap.url}</span>. Dizin kullanıcıları ilk girişte burada bir
              hesap edinir; rolleri aşağıdaki grup eşlemelerinden gelir ve her girişte yenilenir.
              {ldap.default_role
                ? ` Eşleşen grubu olmayanlar "${ldap.default_role}" rolünü alır.`
                : " Eşleşen grubu olmayanlar giriş yapamaz."}
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
            LDAP kapalı. Açmak için backend&apos;e <code className="font-mono">UPTIME_LDAP_URL</code> ve ilgili
            ayarları verin; README&apos;de anlatılıyor.
          </p>
        )}
      </section>
    </>
  );
}

export default function Page() {
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Kullanıcılar</h1>
        <p className="mt-1 text-sm text-muted">
          Yöneticiler her şeyi yönetir; editörler ve izleyiciler yalnızca kendi namespace&apos;lerini görür.
        </p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">Yükleniyor…</p>}>
        <Users />
      </Suspense>
    </main>
  );
}
