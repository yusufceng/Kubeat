"use client";

import { useActionState } from "react";
import { type LoginState, setupAction } from "@/app/login/actions";

const initialState: LoginState = { error: null };

export function SetupForm() {
  const [state, action, pending] = useActionState(setupAction, initialState);

  return (
    <form action={action} className="flex flex-col gap-4">
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Kullanıcı adı</span>
        <input name="username" required autoFocus autoComplete="username" defaultValue="admin" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Görünen ad (isteğe bağlı)</span>
        <input name="display_name" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Parola</span>
        <input name="password" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Parola (yeniden)</span>
        <input name="password_again" type="password" required minLength={8} autoComplete="new-password" className="field" />
      </label>
      {state.error && (
        <p role="alert" className="text-sm text-down">
          {state.error}
        </p>
      )}
      <button type="submit" disabled={pending} className="btn btn-primary">
        {pending ? "Oluşturuluyor…" : "Yönetici hesabını oluştur"}
      </button>
    </form>
  );
}
