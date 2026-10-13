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
import { useT } from "@/lib/i18n/client";

export const roleLabels: Record<Role, string> = { admin: "auth.role.admin", editor: "auth.role.editor", viewer: "auth.role.viewer" };

const roleHints: Record<Role, string> = {
  admin: "users.roleHint.admin",
  editor: "users.roleHint.editor",
  viewer: "users.roleHint.viewer",
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
  const t = useT();
  return (
    <>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.role")}</span>
        <select name="role" value={role} onChange={(e) => onRole(e.target.value as Role)} className="field">
          {(Object.keys(roleLabels) as Role[]).map((r) => (
            <option key={r} value={r}>
              {t(roleLabels[r])}
            </option>
          ))}
        </select>
        <span className="mt-1 block text-xs text-muted">{t(roleHints[role])}</span>
      </label>
      {role !== "admin" && (
        <label className="sm:col-span-4">
          <span className="mb-1 block text-xs font-medium text-muted">{t("users.namespaces.label")}</span>
          <input name="namespaces" required defaultValue={namespaces} placeholder={t("users.namespaces.placeholder")} className="field font-mono" />
          <span className="mt-1 block text-xs text-muted">
            {t("users.namespaces.hintBefore")} <code className="font-mono">*</code> {t("users.namespaces.hintAfter")}
            {known.length > 0 && ` ${t("users.namespaces.known", { list: known.join(", ") })}`}
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
  const t = useT();

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
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.username")}</span>
        <input name="username" required autoComplete="off" placeholder={t("users.new.usernamePlaceholder")} className="field" />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.displayName")}</span>
        <input name="display_name" placeholder={t("users.new.displayNamePlaceholder")} className="field" />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.email")}</span>
        <input name="email" type="email" className="field" />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.password")}</span>
        <input name="password" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <RoleFields role={role} namespaces="" known={known} onRole={setRole} />
      <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? t("auth.creating") : t("users.new.submit")}
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
  const t = useT();
  // Accounts that sign in elsewhere: their role and password are not ours to set.
  const ldap = user.source !== "local";
  const sourceBadge = user.source === "ldap" ? t("users.row.ldap") : user.source === "oidc" ? t("users.row.sso") : null;

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
            <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal">{t(roleLabels[user.role])}</span>
            {sourceBadge && <span className="rounded bg-maint/12 px-1.5 py-0.5 text-xs font-normal text-maint">{sourceBadge}</span>}
            {!user.enabled && <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal text-muted">{t("users.row.disabled")}</span>}
            {self && <span className="text-xs font-normal text-muted">{t("users.row.you")}</span>}
          </div>
          <div className="mt-0.5 text-xs text-muted">
            <span className="font-mono">{user.username}</span>
            {user.email && ` · ${user.email}`}
            {user.role !== "admin" && ` · ${user.namespaces.join(", ") || t("common.none")}`}
            {" · "}
            {user.last_login_at ? (
              <>
                {t("users.row.lastSignIn")} <LocalTime iso={user.last_login_at} seconds={false} />
              </>
            ) : (
              t("users.row.neverSignedIn")
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <button type="button" onClick={() => setEditing((v) => !v)} aria-expanded={editing} className="btn" title={t("common.edit")}>
            <EditIcon />
            <span className="sr-only">{t("common.edit")}</span>
          </button>
          {!self && (
            <form
              action={deleteUserAction}
              onSubmit={(e) => {
                if (!confirm(t("users.row.confirmDelete", { username: user.username }))) e.preventDefault();
              }}
            >
              <input type="hidden" name="id" value={user.id} />
              <button type="submit" className="btn btn-danger" title={t("common.delete")}>
                <TrashIcon />
                <span className="sr-only">{t("common.delete")}</span>
              </button>
            </form>
          )}
        </div>
      </div>
      {editing && (
        <form onSubmit={submit} className="mt-3 grid gap-4 rounded-lg bg-sunken/60 p-4 sm:grid-cols-6">
          <input type="hidden" name="id" value={user.id} />
          <label className="sm:col-span-3">
            <span className="mb-1 block text-xs font-medium text-muted">{t("auth.displayName")}</span>
            <input name="display_name" defaultValue={user.display_name} className="field" />
          </label>
          <label className="sm:col-span-3">
            <span className="mb-1 block text-xs font-medium text-muted">{t("auth.email")}</span>
            <input name="email" type="email" defaultValue={user.email} className="field" />
          </label>
          {ldap ? (
            <p className="text-xs text-muted sm:col-span-6">{t("users.row.external")}</p>
          ) : (
            <>
              <RoleFields role={role} namespaces={user.namespaces.join(", ")} known={known} onRole={setRole} />
              <label className="sm:col-span-3">
                <span className="mb-1 block text-xs font-medium text-muted">{t("users.row.newPassword")}</span>
                <input name="password" type="password" minLength={8} autoComplete="new-password" className="field" />
              </label>
            </>
          )}
          <label className="flex items-center gap-2 text-sm sm:col-span-3">
            <input type="checkbox" name="enabled" defaultChecked={user.enabled} disabled={self} className="h-4 w-4 accent-(--accent)" />
            {t("users.row.enabled")}
          </label>
          <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
            <button type="submit" disabled={pending} className="btn btn-primary">
              {pending ? t("common.saving") : t("common.save")}
            </button>
            <button type="button" onClick={() => setEditing(false)} className="btn">
              {t("common.cancel")}
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
  const t = useT();

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
        <span className="mb-1 block text-xs font-medium text-muted">{t("users.group.dn")}</span>
        <input name="group_dn" required placeholder={t("users.group.dnPlaceholder")} className="field font-mono" />
      </label>
      <RoleFields role={role} namespaces="" known={known} onRole={setRole} />
      <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? t("users.group.adding") : t("users.group.submit")}
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
  const t = useT();
  return (
    <li className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
      <div className="min-w-0">
        <div className="truncate font-mono text-sm">{group.group_dn}</div>
        <div className="mt-0.5 text-xs text-muted">
          {t(roleLabels[group.role])}
          {group.role !== "admin" && ` · ${group.namespaces.join(", ")}`}
        </div>
      </div>
      <form
        action={deleteGroupRoleAction}
        onSubmit={(e) => {
          if (!confirm(t("users.group.confirmDelete"))) e.preventDefault();
        }}
      >
        <input type="hidden" name="id" value={group.id} />
        <button type="submit" className="btn btn-danger" title={t("common.delete")}>
          <TrashIcon />
          <span className="sr-only">{t("common.delete")}</span>
        </button>
      </form>
    </li>
  );
}

/** Changes the signed-in user's own password. */
export function PasswordForm() {
  const [state, action, pending] = useActionState<Outcome, FormData>(changePasswordAction, undefined);
  const t = useT();
  return (
    <form action={action} className="flex max-w-sm flex-col gap-4">
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("account.currentPassword")}</span>
        <input name="current_password" type="password" required autoComplete="current-password" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("account.newPassword")}</span>
        <input name="new_password" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("account.newPasswordAgain")}</span>
        <input name="new_password_again" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <div className="flex items-center gap-3">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? t("common.saving") : t("account.changePassword")}
        </button>
        {state && !pending && (
          <p role="status" className={`text-sm ${state.error ? "text-down" : "text-up"}`}>
            {state.error || t("account.passwordChanged")}
          </p>
        )}
      </div>
    </form>
  );
}
