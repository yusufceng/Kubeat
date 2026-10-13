"use client";

import { useActionState, useState, useTransition } from "react";
import {
  changePasswordAction,
  createGroupRoleAction,
  createUserAction,
  deleteGroupRoleAction,
  deleteUserAction,
  type Outcome,
  updateUserAction,
} from "@/app/actions";
import type { GroupRole, Role, User } from "@/lib/api";
import { EditIcon, TrashIcon } from "@/app/ui/icons";
import { LocalTime } from "@/app/ui/local-time";

export const roleLabels: Record<Role, string> = { admin: "Yönetici", editor: "Editör", viewer: "İzleyici" };

const roleHints: Record<Role, string> = {
  admin: "Her şeyi görür ve değiştirir; kullanıcıları ve kanalları yönetir.",
  editor: "Kendi namespace'lerindeki monitor'ları, kanalları, bakım pencerelerini ve durum sayfalarını yönetir.",
  viewer: "Kendi namespace'lerindeki monitor'ları ve olayları görür; hiçbir şeyi değiştiremez.",
};

function RoleFields({
  role,
  namespaces,
  known,
  onRole,
}: {
  role: Role;
  namespaces: string;
  /** The namespaces and groups that have monitors, as a hint. */
  known: string[];
  onRole: (role: Role) => void;
}) {
  return (
    <>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">Rol</span>
        <select name="role" value={role} onChange={(e) => onRole(e.target.value as Role)} className="field">
          {(Object.keys(roleLabels) as Role[]).map((r) => (
            <option key={r} value={r}>
              {roleLabels[r]}
            </option>
          ))}
        </select>
        <span className="mt-1 block text-xs text-muted">{roleHints[role]}</span>
      </label>
      {role !== "admin" && (
        <label className="sm:col-span-4">
          <span className="mb-1 block text-xs font-medium text-muted">Namespace&apos;ler ve gruplar (virgülle)</span>
          <input name="namespaces" required defaultValue={namespaces} placeholder="shop, billing" className="field font-mono" />
          <span className="mt-1 block text-xs text-muted">
            Hepsi için <code className="font-mono">*</code> yazın.
            {known.length > 0 && ` Mevcut: ${known.join(", ")}`}
          </span>
        </label>
      )}
    </>
  );
}

/** Creates a local account. */
export function NewUserForm({ known }: { known: string[] }) {
  const [role, setRole] = useState<Role>("viewer");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    setError(null);
    startTransition(async () => {
      const result = await createUserAction(data);
      if (result?.error) setError(result.error);
      else form.reset();
    });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 sm:grid-cols-6">
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">Kullanıcı adı</span>
        <input name="username" required autoComplete="off" placeholder="ayse" className="field" />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">Görünen ad</span>
        <input name="display_name" placeholder="Ayşe Yılmaz" className="field" />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">E-posta</span>
        <input name="email" type="email" className="field" />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">Parola</span>
        <input name="password" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <RoleFields role={role} namespaces="" known={known} onRole={setRole} />
      <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? "Oluşturuluyor…" : "Kullanıcı oluştur"}
        </button>
        {error && (
          <p role="alert" className="text-sm text-down">
            {error}
          </p>
        )}
      </div>
    </form>
  );
}

/** One account in the list, with its editing form. */
export function UserRow({ user, self, known }: { user: User; self: boolean; known: string[] }) {
  const [editing, setEditing] = useState(false);
  const [role, setRole] = useState<Role>(user.role);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  // Accounts that sign in elsewhere: their role and password are not ours to set.
  const ldap = user.source !== "local";
  const sourceBadge = user.source === "ldap" ? "LDAP" : user.source === "oidc" ? "SSO" : null;

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    setError(null);
    startTransition(async () => {
      const result = await updateUserAction(data);
      if (result?.error) setError(result.error);
      else setEditing(false);
    });
  }

  return (
    <li className="px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
            <span className={user.enabled ? "" : "text-muted line-through"}>{user.display_name || user.username}</span>
            <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal">{roleLabels[user.role]}</span>
            {sourceBadge && <span className="rounded bg-maint/12 px-1.5 py-0.5 text-xs font-normal text-maint">{sourceBadge}</span>}
            {!user.enabled && <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal text-muted">Devre dışı</span>}
            {self && <span className="text-xs font-normal text-muted">(siz)</span>}
          </div>
          <div className="mt-0.5 text-xs text-muted">
            <span className="font-mono">{user.username}</span>
            {user.email && ` · ${user.email}`}
            {user.role !== "admin" && ` · ${user.namespaces.join(", ") || "–"}`}
            {" · "}
            {user.last_login_at ? (
              <>
                son giriş <LocalTime iso={user.last_login_at} seconds={false} />
              </>
            ) : (
              "hiç giriş yapmadı"
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button type="button" onClick={() => setEditing((v) => !v)} aria-expanded={editing} className="btn" title="Düzenle">
            <EditIcon />
            <span className="sr-only">Düzenle</span>
          </button>
          {!self && (
            <form
              action={deleteUserAction}
              onSubmit={(e) => {
                if (!confirm(`"${user.username}" hesabı silinsin mi?`)) e.preventDefault();
              }}
            >
              <input type="hidden" name="id" value={user.id} />
              <button type="submit" className="btn btn-danger" title="Sil">
                <TrashIcon />
                <span className="sr-only">Sil</span>
              </button>
            </form>
          )}
        </div>
      </div>
      {editing && (
        <form onSubmit={submit} className="mt-3 grid gap-4 rounded-lg bg-sunken/60 p-4 sm:grid-cols-6">
          <input type="hidden" name="id" value={user.id} />
          <label className="sm:col-span-3">
            <span className="mb-1 block text-xs font-medium text-muted">Görünen ad</span>
            <input name="display_name" defaultValue={user.display_name} className="field" />
          </label>
          <label className="sm:col-span-3">
            <span className="mb-1 block text-xs font-medium text-muted">E-posta</span>
            <input name="email" type="email" defaultValue={user.email} className="field" />
          </label>
          {ldap ? (
            <p className="text-xs text-muted sm:col-span-6">
              Rol ve kapsam dizindeki gruplarından, parola dizinden gelir; burada değiştirilemez.
            </p>
          ) : (
            <>
              <RoleFields role={role} namespaces={user.namespaces.join(", ")} known={known} onRole={setRole} />
              <label className="sm:col-span-3">
                <span className="mb-1 block text-xs font-medium text-muted">Yeni parola (boş: değişmez)</span>
                <input name="password" type="password" minLength={8} autoComplete="new-password" className="field" />
              </label>
            </>
          )}
          <label className="flex items-center gap-2 text-sm sm:col-span-3">
            <input type="checkbox" name="enabled" defaultChecked={user.enabled} disabled={self} className="h-4 w-4 accent-(--accent)" />
            Hesap etkin
          </label>
          <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
            <button type="submit" disabled={pending} className="btn btn-primary">
              {pending ? "Kaydediliyor…" : "Kaydet"}
            </button>
            <button type="button" onClick={() => setEditing(false)} className="btn">
              Vazgeç
            </button>
            {error && (
              <p role="alert" className="text-sm text-down">
                {error}
              </p>
            )}
          </div>
        </form>
      )}
    </li>
  );
}

/** Maps a directory group to a role. */
export function GroupRoleForm({ known }: { known: string[] }) {
  const [role, setRole] = useState<Role>("viewer");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    setError(null);
    startTransition(async () => {
      const result = await createGroupRoleAction(data);
      if (result?.error) setError(result.error);
      else form.reset();
    });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 sm:grid-cols-6">
      <label className="sm:col-span-6">
        <span className="mb-1 block text-xs font-medium text-muted">Grubun DN&apos;i</span>
        <input name="group_dn" required placeholder="cn=shop-team,ou=groups,dc=example,dc=com" className="field font-mono" />
      </label>
      <RoleFields role={role} namespaces="" known={known} onRole={setRole} />
      <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? "Ekleniyor…" : "Eşleme ekle"}
        </button>
        {error && (
          <p role="alert" className="text-sm text-down">
            {error}
          </p>
        )}
      </div>
    </form>
  );
}

export function GroupRoleRow({ group }: { group: GroupRole }) {
  return (
    <li className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
      <div className="min-w-0">
        <div className="truncate font-mono text-sm">{group.group_dn}</div>
        <div className="mt-0.5 text-xs text-muted">
          {roleLabels[group.role]}
          {group.role !== "admin" && ` · ${group.namespaces.join(", ")}`}
        </div>
      </div>
      <form
        action={deleteGroupRoleAction}
        onSubmit={(e) => {
          if (!confirm("Bu eşleme silinsin mi? Bu gruptaki kullanıcılar bir sonraki girişte rolünü kaybeder.")) e.preventDefault();
        }}
      >
        <input type="hidden" name="id" value={group.id} />
        <button type="submit" className="btn btn-danger" title="Sil">
          <TrashIcon />
          <span className="sr-only">Sil</span>
        </button>
      </form>
    </li>
  );
}

/** Changes the signed-in user's own password. */
export function PasswordForm() {
  const [state, action, pending] = useActionState<Outcome, FormData>(changePasswordAction, undefined);
  return (
    <form action={action} className="flex max-w-sm flex-col gap-4">
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Mevcut parola</span>
        <input name="current_password" type="password" required autoComplete="current-password" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Yeni parola</span>
        <input name="new_password" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Yeni parola (yeniden)</span>
        <input name="new_password_again" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <div className="flex items-center gap-3">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? "Kaydediliyor…" : "Parolayı değiştir"}
        </button>
        {state && !pending && (
          <p role="status" className={`text-sm ${state.error ? "text-down" : "text-up"}`}>
            {state.error || "Parola değiştirildi. Diğer oturumlarınız kapatıldı."}
          </p>
        )}
      </div>
    </form>
  );
}
