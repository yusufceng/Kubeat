"use server";

import { redirect } from "next/navigation";
import { ApiError, login, logout, setup } from "@/lib/api";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { clearSessionCookie, sessionToken, setSessionCookie } from "@/lib/session";

export type LoginState = { error: string | null };

/** Only same-site paths are followed after sign-in. */
function safeNext(value: FormDataEntryValue | null): string {
  const next = typeof value === "string" ? value : "";
  return next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/\\") ? next : "/";
}

function refusal(err: unknown, t: T): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 401:
        return t("login.refusal.credentials");
      case 403:
        return err.message.includes("disabled") ? t("login.refusal.disabled") : t("login.refusal.noRole");
      case 429:
        return t("login.refusal.tooMany");
      case 502:
        return t("login.refusal.directory");
      case 409:
        return t("login.refusal.exists");
      case 400:
        return t("login.refusal.invalid", { message: err.message });
    }
  }
  return t("login.refusal.backend");
}

export async function loginAction(_prev: LoginState, formData: FormData): Promise<LoginState> {
  const t = await getT();
  let session;
  try {
    session = await login(String(formData.get("username") ?? "").trim(), String(formData.get("password") ?? ""));
  } catch (err) {
    return { error: refusal(err, t) };
  }
  await setSessionCookie(session.token, session.expires_at);
  redirect(safeNext(formData.get("next")));
}

/** Creates the first administrator and signs them in. */
export async function setupAction(_prev: LoginState, formData: FormData): Promise<LoginState> {
  const t = await getT();
  const password = String(formData.get("password") ?? "");
  if (password !== String(formData.get("password_again") ?? "")) {
    return { error: t("setup.passwordMismatch") };
  }
  let session;
  try {
    session = await setup({
      username: String(formData.get("username") ?? "").trim(),
      display_name: String(formData.get("display_name") ?? "").trim(),
      password,
    });
  } catch (err) {
    return { error: refusal(err, t) };
  }
  await setSessionCookie(session.token, session.expires_at);
  redirect("/");
}

export async function logoutAction(): Promise<void> {
  const token = await sessionToken();
  if (token) await logout(token).catch(() => undefined); // gone is gone
  await clearSessionCookie();
  redirect("/login");
}
