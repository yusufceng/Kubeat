"use client";

import { useActionState } from "react";
import { type LoginState, setupAction } from "@/app/login/actions";
import { useT } from "@/lib/i18n/client";

const initialState: LoginState = { error: null };

export function SetupForm() {
  const [state, action, pending] = useActionState(setupAction, initialState);
  const t = useT();

  return (
    <form action={action} className="flex flex-col gap-4">
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.username")}</span>
        <input name="username" required autoFocus autoComplete="username" defaultValue="admin" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("setup.displayName")}</span>
        <input name="display_name" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("auth.password")}</span>
        <input name="password" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">{t("setup.passwordAgain")}</span>
        <input name="password_again" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      {state.error && (
        <p role="alert" className="text-sm text-down">
          {state.error}
        </p>
      )}
      <button type="submit" disabled={pending} className="btn btn-primary">
        {pending ? t("auth.creating") : t("setup.submit")}
      </button>
    </form>
  );
}
