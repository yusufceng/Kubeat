"use client";

import { useActionState, useState } from "react";
import { type LoginState, setupAction } from "@/app/login/actions";
import { useT } from "@/lib/i18n/client";

const initialState: LoginState = { error: null };

/**
 * The first administrator: a name to sign in with, one to be shown, and a
 * password typed twice; whether the two match is said as they are typed.
 */
export function SetupForm() {
  const [state, action, pending] = useActionState(setupAction, initialState);
  const t = useT();
  // Kept by the form, so that a refused attempt leaves what was typed.
  const [username, setUsername] = useState("admin");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [again, setAgain] = useState("");
  const mismatch = again.length > 0 && password !== again;

  return (
    <form action={action} className="panel auth-panel flex flex-col gap-4">
      <label>
        <span className="pg-label">{t("auth.username")}</span>
        <input
          name="username"
          required
          autoFocus
          autoComplete="username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          className="field font-mono"
        />
      </label>
      <label>
        <span className="pg-label">
          {t("auth.displayName")}
          <small>{t("form.optional")}</small>
        </span>
        <input name="display_name" value={displayName} onChange={(e) => setDisplayName(e.target.value)} className="field" />
      </label>
      <label>
        <span className="pg-label">{t("auth.password")}</span>
        <input
          name="password"
          type="password"
          required
          minLength={8}
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="field"
        />
        <span className="pg-help">{t("users.passwordHint")}</span>
      </label>
      <label>
        <span className="pg-label">{t("setup.passwordAgain")}</span>
        <input
          name="password_again"
          type="password"
          required
          minLength={8}
          autoComplete="new-password"
          value={again}
          onChange={(e) => setAgain(e.target.value)}
          aria-invalid={mismatch || undefined}
          aria-describedby="setup-match"
          className="field"
        />
        <span id="setup-match" className={`pg-help ${mismatch ? "text-warn-ink" : ""}`} aria-live="polite">
          {mismatch ? t("setup.passwordMismatch") : ""}
        </span>
      </label>
      {state.error && (
        <p role="alert" className="text-[13px] leading-[18px] text-down-ink">
          {state.error}
        </p>
      )}
      <button type="submit" disabled={pending} className="btn btn-primary w-full">
        {pending ? t("auth.creating") : t("setup.submit")}
      </button>
    </form>
  );
}
