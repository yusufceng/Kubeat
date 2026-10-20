"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
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
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { ConfirmForm } from "@/app/ui/confirm";
import { EditIcon, TrashIcon } from "@/app/ui/icons";
import { StatusGlyph } from "@/app/ui/status-glyph";
import { TableTime } from "@/app/ui/table-time";

export const roleLabels: Record<Role, string> = { admin: "auth.role.admin", editor: "auth.role.editor", viewer: "auth.role.viewer" };

const roleHints: Record<Role, string> = {
  admin: "users.roleHint.admin",
  editor: "users.roleHint.editor",
  viewer: "users.roleHint.viewer",
};

const roles = Object.keys(roleLabels) as Role[];

/** "shop, billing" -> ["shop", "billing"], as the server reads the field. */
function namespaceList(text: string): string[] {
  return text
    .split(",")
    .map((ns) => ns.trim())
    .filter(Boolean);
}

/** What an account may see and do, as a sentence: the role and the namespaces read back. */
function accessSentence(name: string, role: Role, namespaces: string[], t: T): React.ReactNode {
  const who = name.trim() || t("users.echo.someone");
  if (role === "admin") return t("users.echo.admin", { name: who });
  if (namespaces.length === 0) return t("users.echo.noScope");
  const scope = namespaces.includes("*")
    ? t("users.echo.scope.all")
    : t.n("users.echo.scope.some", namespaces.length, {
        namespaces: new Intl.ListFormat(t.intl, { type: "conjunction" }).format(namespaces),
      });
  const [before, middle] = t(role === "editor" ? "users.echo.editor" : "users.echo.viewer", { name: who }).split("{scope}");
  return (
    <>
      {before}
      <b>{scope}</b>
      {middle}
    </>
  );
}

/**
 * The role of an account and, for all but administrators, the namespaces
 * it covers. The namespaces that have monitors are offered as chips that
 * add themselves to the field.
 */
function AccessFields({
  role,
  namespaces,
  known,
  onRole,
  onNamespaces,
}: {
  role: Role;
  namespaces: string;
  /** The namespaces and groups that have monitors, as a hint. */
  known: string[];
  onRole: (role: Role) => void;
  onNamespaces: (value: string) => void;
}) {
  const t = useT();
  const listed = namespaceList(namespaces);
  const add = (ns: string) => onNamespaces([...listed.filter((n) => n !== ns), ns].join(", "));
  return (
    <>
      <fieldset>
        <legend className="pg-label">{t("auth.role")}</legend>
        <div className="pg-seg">
          {roles.map((r) => (
            <label key={r}>
              <input type="radio" name="role" value={r} checked={role === r} onChange={() => onRole(r)} />
              {t(roleLabels[r])}
            </label>
          ))}
        </div>
        <span className="pg-help">{t(roleHints[role])}</span>
      </fieldset>
      {role !== "admin" && (
        <label>
          <span className="pg-label">{t("users.namespaces.label")}</span>
          <input
            name="namespaces"
            required
            value={namespaces}
            onChange={(e) => onNamespaces(e.target.value)}
            placeholder={t("users.namespaces.placeholder")}
            className="field font-mono"
          />
          <span className="pg-help">
            {t("users.namespaces.hintBefore")} <code className="font-mono">*</code> {t("users.namespaces.hintAfter")}
          </span>
        </label>
      )}
      {role !== "admin" && known.length > 0 && (
        <div className="pg-chips" role="group" aria-label={t("users.namespaces.add")}>
          {["*", ...known].map((ns) => (
            <button
              key={ns}
              type="button"
              className="pg-chip pg-chip-mono"
              aria-pressed={listed.includes(ns)}
              onClick={() => add(ns)}
              title={t("users.namespaces.addOne", { namespace: ns })}
            >
              {ns === "*" ? `* ${t("users.namespaces.allChip")}` : ns}
            </button>
          ))}
        </div>
      )}
    </>
  );
}

/**
 * The form of an account. Without a user it creates a local account; with
 * one it edits it, and an account that signs in elsewhere (LDAP, SSO)
 * keeps the role and password its directory gives it. closeHref is where
 * Cancel and a successful save lead.
 */
export function UserForm({
  user,
  self = false,
  known,
  closeHref,
}: {
  user?: User;
  self?: boolean;
  known: string[];
  closeHref: string;
}) {
  const t = useT();
  const router = useRouter();
  const [role, setRole] = useState<Role>(user?.role ?? "viewer");
  const [namespaces, setNamespaces] = useState(user?.namespaces.join(", ") ?? "");
  // Who the sentence under the access fields speaks of.
  const [username, setUsername] = useState(user?.username ?? "");
  const [displayName, setDisplayName] = useState(user?.display_name ?? "");
  const name = displayName.trim() || username;
  const [enabled, setEnabled] = useState(user?.enabled ?? true);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  // Accounts that sign in elsewhere: their role and password are not ours to set.
  const external = user !== undefined && user.source !== "local";

  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    setError(null);
    startTransition(async () => {
      const result = user ? await updateUserAction(data) : await createUserAction(data);
      if (result?.error) {
        setError(result.error);
        return;
      }
      router.replace(closeHref, { scroll: false });
    });
  }

  return (
    <form onSubmit={submit} className="pg-form">
      {user && <input type="hidden" name="id" value={user.id} />}
      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("users.sec.account")}</h2>
          <p>{user ? t("users.sec.accountEdit") : t("users.new.hint")}</p>
        </div>
        <div className="pg-fields">
          <div className="pg-grid">
            {!user && (
              <label className="pg-span-3">
                <span className="pg-label">{t("auth.username")}</span>
                <input
                  name="username"
                  required
                  autoComplete="off"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder={t("users.new.usernamePlaceholder")}
                  className="field font-mono"
                />
              </label>
            )}
            <label className="pg-span-3">
              <span className="pg-label">
                {t("auth.displayName")}
                <small>{t("form.optional")}</small>
              </span>
              <input
                name="display_name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder={t("users.new.displayNamePlaceholder")}
                className="field"
              />
            </label>
            <label className="pg-span-3">
              <span className="pg-label">
                {t("auth.email")}
                <small>{t("form.optional")}</small>
              </span>
              <input name="email" type="email" defaultValue={user?.email} className="field" />
            </label>
            {!external && (
              <label className="pg-span-3">
                <span className="pg-label">
                  {user ? t("account.newPassword") : t("auth.password")}
                  {user && <small>{t("users.row.passwordKept")}</small>}
                </span>
                <input name="password" type="password" required={!user} minLength={8} autoComplete="new-password" className="field" />
                <span className="pg-help">{t("users.passwordHint")}</span>
              </label>
            )}
          </div>
        </div>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("users.sec.access")}</h2>
          <p>{t("users.sec.accessBody")}</p>
        </div>
        <div className="pg-fields">
          {external ? (
            <p className="pg-help mt-0">{t("users.row.external")}</p>
          ) : (
            <AccessFields role={role} namespaces={namespaces} known={known} onRole={setRole} onNamespaces={setNamespaces} />
          )}
          <p className="pg-echo" aria-live="polite">
            {accessSentence(name, external ? user.role : role, external ? user.namespaces : namespaceList(namespaces), t)}
          </p>
        </div>
      </div>

      {user && (
        <div className="pg-sec">
          <div className="pg-sec-intro">
            <h2>{t("users.sec.state")}</h2>
          </div>
          <div className="pg-fields">
            <label className="pg-check">
              <input
                type="checkbox"
                name="enabled"
                checked={enabled}
                disabled={self}
                onChange={(e) => setEnabled(e.target.checked)}
              />
              <span>
                <b>{t("users.row.enabled")}</b>
                <small>{self ? t("users.row.selfEnabled") : enabled ? t("users.row.enabledOn") : t("users.row.enabledOff")}</small>
              </span>
            </label>
            {/* A box that cannot be changed is not sent; the account stays enabled. */}
            {self && <input type="hidden" name="enabled" value="on" />}
          </div>
        </div>
      )}

      <div className="pg-savebar">
        {error && <p role="alert">{error}</p>}
        <div className="pg-savebar-end">
          <Link href={closeHref} scroll={false} className="btn btn-ghost">
            {t("common.cancel")}
          </Link>
          <button type="submit" disabled={pending} className="btn btn-primary">
            {pending ? (user ? t("common.saving") : t("auth.creating")) : user ? t("common.save") : t("users.new.submit")}
          </button>
        </div>
      </div>
    </form>
  );
}

/** Creates a local account. */
export function NewUserForm({ known }: { known: string[] }) {
  return <UserForm known={known} closeHref="/users" />;
}

/**
 * One account as a row of the table: who it is, its role and scope, where
 * it signs in, when it last did, and what can be done with it. While it is
 * edited, its form opens in a row of its own under it.
 */
export function UserRow({
  user,
  self,
  known,
  editing = false,
}: {
  user: User;
  self: boolean;
  known: string[];
  editing?: boolean;
}) {
  const t = useT();
  const source = user.source === "ldap" ? t("users.row.ldap") : user.source === "oidc" ? t("users.row.sso") : t("account.source.local");
  const scope =
    user.role === "admin"
      ? t("users.row.everything")
      : user.namespaces.includes("*")
        ? t("users.row.allNamespaces")
        : user.namespaces.join(", ") || t("common.none");
  const name = user.display_name || user.username;

  return (
    <>
      <tr className={editing ? "pg-row-open" : undefined}>
        <td className="pg-grow">
          <span className="flex min-w-0 items-baseline gap-x-2 gap-y-1 max-sm:flex-wrap">
            <span className={`pg-trunc pg-name ${user.enabled ? "" : "text-ink-3"}`} title={name}>
              {name}
            </span>
            {user.display_name && (
              <span className="pg-trunc pg-mono flex-none max-w-48 text-ink-3" title={user.username}>
                {user.username}
              </span>
            )}
            {self && <span className="pg-tag flex-none">{t("users.row.you")}</span>}
            {!user.enabled && (
              <span className="pg-state flex-none text-ink-3">
                <StatusGlyph kind="paused" />
                {t("users.row.disabled")}
              </span>
            )}
          </span>
        </td>
        <td className="pg-shrink" data-label={`${t("auth.role")}:`}>
          {t(roleLabels[user.role])}
        </td>
        <td className="pg-shrink" data-label={`${t("users.col.scope")}:`}>
          <span className={`pg-trunc max-w-60 ${user.role === "admin" ? "text-ink-2" : "pg-mono text-ink-2"}`} title={scope}>
            {scope}
          </span>
        </td>
        <td className="pg-shrink text-ink-2" data-label={`${t("users.col.source")}:`}>
          {source}
        </td>
        <td className="pg-time pg-shrink" data-label={`${t("users.col.lastSignIn")}:`}>
          {user.last_login_at ? <TableTime iso={user.last_login_at} /> : <span className="text-ink-3">{t("users.row.neverSignedIn")}</span>}
        </td>
        <td className="pg-actions">
          <span className="pg-row-actions">
            <Link
              href={editing ? "/users" : `/users?edit=${user.id}`}
              scroll={false}
              className="pg-icon-btn"
              aria-label={`${t("common.edit")}: ${name}`}
              aria-expanded={editing}
              title={t("common.edit")}
            >
              <EditIcon />
            </Link>
            {!self && (
              <ConfirmForm
                action={deleteUserAction}
                className="contents"
                title={t("users.row.deleteTitle", { username: user.username })}
                question={t("users.row.deleteBody")}
                items={[`${user.username} · ${t(roleLabels[user.role])} · ${source}`]}
                icon={<TrashIcon />}
              >
                <input type="hidden" name="id" value={user.id} />
                <button
                  type="submit"
                  className="pg-icon-btn pg-icon-btn-danger"
                  aria-label={`${t("common.delete")}: ${name}`}
                  title={t("common.delete")}
                >
                  <TrashIcon />
                </button>
              </ConfirmForm>
            )}
          </span>
        </td>
      </tr>
      {editing && (
        <tr className="pg-row-form">
          <td colSpan={6}>
            <div className="pg-inline-form">
              <UserForm user={user} self={self} known={known} closeHref="/users" />
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

/** Maps a directory group to a role. */
export function GroupRoleForm({ known }: { known: string[] }) {
  const [role, setRole] = useState<Role>("viewer");
  const [namespaces, setNamespaces] = useState("");
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
      if (result?.error) {
        setError(result.error);
        return;
      }
      form.reset();
      setNamespaces("");
      setRole("viewer");
    });
  }

  return (
    <form onSubmit={submit} className="pg-form">
      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h3>{t("users.group.title")}</h3>
          <p>{t("users.group.body")}</p>
        </div>
        <div className="pg-fields">
          <label>
            <span className="pg-label">{t("users.group.dn")}</span>
            <input name="group_dn" required placeholder={t("users.group.dnPlaceholder")} className="field font-mono" />
          </label>
          <AccessFields role={role} namespaces={namespaces} known={known} onRole={setRole} onNamespaces={setNamespaces} />
        </div>
      </div>
      <div className="pg-savebar">
        {error && <p role="alert">{error}</p>}
        <div className="pg-savebar-end">
          <button type="submit" disabled={pending} className="btn btn-primary">
            {pending ? t("users.group.adding") : t("users.group.submit")}
          </button>
        </div>
      </div>
    </form>
  );
}

/** One group mapping as a row: the group's DN, the role it gives, and its scope. */
export function GroupRoleRow({ group }: { group: GroupRole }) {
  const t = useT();
  const scope = group.role === "admin" ? t("users.row.everything") : group.namespaces.join(", ");
  return (
    <tr>
      <td className="pg-grow">
        <span className="pg-trunc pg-mono" title={group.group_dn}>
          {group.group_dn}
        </span>
      </td>
      <td className="pg-shrink" data-label={`${t("auth.role")}:`}>
        {t(roleLabels[group.role])}
      </td>
      <td className="pg-shrink" data-label={`${t("users.col.scope")}:`}>
        <span className={`pg-trunc max-w-60 text-ink-2 ${group.role === "admin" ? "" : "pg-mono"}`} title={scope}>
          {scope}
        </span>
      </td>
      <td className="pg-actions">
        <span className="pg-row-actions">
          <ConfirmForm
            action={deleteGroupRoleAction}
            className="contents"
            title={t("users.group.deleteTitle")}
            question={t("users.group.confirmDelete")}
            items={[group.group_dn]}
            icon={<TrashIcon />}
          >
            <input type="hidden" name="id" value={group.id} />
            <button
              type="submit"
              className="pg-icon-btn pg-icon-btn-danger"
              aria-label={`${t("common.delete")}: ${group.group_dn}`}
              title={t("common.delete")}
            >
              <TrashIcon />
            </button>
          </ConfirmForm>
        </span>
      </td>
    </tr>
  );
}

/** Changes the signed-in user's own password, in a section of its own. */
export function PasswordForm() {
  const [state, action, pending] = useActionState<Outcome, FormData>(changePasswordAction, undefined);
  const t = useT();
  return (
    <form action={action} className="pg-form">
      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("auth.password")}</h2>
          <p>{t("account.passwordBody")}</p>
        </div>
        <div className="pg-fields">
          <label className="max-w-sm">
            <span className="pg-label">{t("account.currentPassword")}</span>
            <input name="current_password" type="password" required autoComplete="current-password" className="field" />
          </label>
          <div className="pg-grid">
            <label className="pg-span-3">
              <span className="pg-label">{t("account.newPassword")}</span>
              <input name="new_password" type="password" required minLength={8} autoComplete="new-password" className="field" />
              <span className="pg-help">{t("users.passwordHint")}</span>
            </label>
            <label className="pg-span-3">
              <span className="pg-label">{t("account.newPasswordAgain")}</span>
              <input name="new_password_again" type="password" required minLength={8} autoComplete="new-password" className="field" />
            </label>
          </div>
        </div>
      </div>
      <div className="pg-savebar">
        {state && !pending && state.error && <p role="alert">{state.error}</p>}
        {state && !pending && !state.error && (
          <p role="status" className="min-w-0 flex-1 text-[13px] text-up-ink">
            {t("account.passwordChanged")}
          </p>
        )}
        <div className="pg-savebar-end">
          <button type="submit" disabled={pending} className="btn btn-primary">
            {pending ? t("common.saving") : t("account.changePassword")}
          </button>
        </div>
      </div>
    </form>
  );
}
