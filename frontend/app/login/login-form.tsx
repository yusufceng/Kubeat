"use client";

import { useActionState } from "react";
import { loginAction, type LoginState } from "@/app/login/actions";

const initialState: LoginState = { error: null };

export function LoginForm({ next, ldap }: { next: string; ldap: boolean }) {
  const [state, action, pending] = useActionState(loginAction, initialState);

  return (
    <form action={action} className="flex flex-col gap-4">
      <input type="hidden" name="next" value={next} />
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Kullanıcı adı</span>
        <input name="username" required autoFocus autoComplete="username" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Parola</span>
        <input name="password" type="password" required autoComplete="current-password" className="field" />
      </label>
      {ldap && <p className="text-xs text-muted">Kurumsal dizin (LDAP) hesabınızla da girebilirsiniz.</p>}
      {state.error && (
        <p role="alert" className="text-sm text-down">
          {state.error}
        </p>
      )}
      <button type="submit" disabled={pending} className="btn btn-primary">
        {pending ? "Giriş yapılıyor…" : "Giriş yap"}
      </button>
    </form>
  );
}
