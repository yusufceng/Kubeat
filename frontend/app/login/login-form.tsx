"use client";

import { useActionState } from "react";
import { loginAction, type LoginState } from "@/app/login/actions";
import { useT } from "@/lib/i18n/client";

const initialState: LoginState = { error: null };

export function LoginForm({ next, ldap }: { next: string; ldap: boolean }) {
  const [state, action, pending] = useActionState(loginAction, initialState);
  const t = useT();

  return (
    <form action={action} className="flex flex-col gap-4">
      <input type="hidden" name="next" value={next} />
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.username")}</span>
        <input name="username" required autoFocus autoComplete="username" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.password")}</span>
        <input name="password" type="password" required autoComplete="current-password" className="field" />
      </label>
      {ldap && <p className="text-xs text-muted">{t("login.ldapHint")}</p>}
      {state.error && (
        <p role="alert" className="text-sm text-down">
          {state.error}
        </p>
      )}
      <button type="submit" disabled={pending} className="btn btn-primary">
        {pending ? t("login.signingIn") : t("login.submit")}
      </button>
    </form>
  );
}
