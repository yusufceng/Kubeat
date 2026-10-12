"use client";

import { useActionState } from "react";
import { loginAction, type LoginState } from "@/app/login/actions";

const initialState: LoginState = { error: null };

export function LoginForm({ next }: { next: string }) {
  const [state, action, pending] = useActionState(loginAction, initialState);

  return (
    <form action={action} className="flex flex-col gap-4">
      <input type="hidden" name="next" value={next} />
      <label>
        <span className="mb-1 block text-xs font-medium text-muted">Yönetici parolası</span>
        <input
          name="password"
          type="password"
          required
          autoFocus
          autoComplete="current-password"
          className="field"
        />
      </label>
      <button
        type="submit"
        disabled={pending}
        className="btn btn-primary"
      >
        {pending ? "Giriş yapılıyor…" : "Giriş yap"}
      </button>
      {state.error && (
        <p role="alert" className="text-sm text-down">
          {state.error}
        </p>
      )}
    </form>
  );
}
