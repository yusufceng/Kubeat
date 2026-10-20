"use client";

import { useActionState, useState } from "react";
import { loginAction, type LoginState } from "@/app/login/actions";
import { useT } from "@/lib/i18n/client";

const initialState: LoginState = { error: null };

export function LoginForm({ next, ldap }: { next: string; ldap: boolean }) {
  const [state, action, pending] = useActionState(loginAction, initialState);
  // Kept by the form, so that a refused sign-in leaves the name typed in.
  const [username, setUsername] = useState("");
  const t = useT();

  return (
    <form action={action} className="flex flex-col gap-4">
      <input type="hidden" name="next" value={next} />
      <label>
        <span className="pg-label">{t("auth.username")}</span>
        <input
          name="username"
          required
          autoFocus
          autoComplete="username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          className="field"
        />
      </label>
      <label>
        <span className="pg-label">{t("auth.password")}</span>
        <input name="password" type="password" required autoComplete="current-password" className="field" />
        {ldap && <span className="pg-help">{t("login.ldapHint")}</span>}
      </label>
      {state.error && (
        <p role="alert" className="text-[13px] leading-[18px] text-down-ink">
          {state.error}
        </p>
      )}
      <button type="submit" disabled={pending} className="btn btn-primary w-full">
        {pending ? t("login.signingIn") : t("login.submit")}
      </button>
    </form>
  );
}
