"use server";

import { redirect } from "next/navigation";
import { ApiError, login, logout, setup } from "@/lib/api";
import { clearSessionCookie, sessionToken, setSessionCookie } from "@/lib/session";

export type LoginState = { error: string | null };

/** Only same-site paths are followed after sign-in. */
function safeNext(value: FormDataEntryValue | null): string {
  const next = typeof value === "string" ? value : "";
  return next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/\\") ? next : "/";
}

function refusal(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 401:
        return "Kullanıcı adı ya da parola hatalı.";
      case 403:
        return err.message.includes("disabled") ? "Bu hesap devre dışı bırakılmış." : "Dizin hesabınızın burada bir rolü yok; bir yöneticiye başvurun.";
      case 429:
        return "Çok fazla hatalı deneme yapıldı. Biraz sonra tekrar deneyin.";
      case 502:
        return "Dizin sunucusuna ulaşılamadı. Biraz sonra tekrar deneyin.";
      case 409:
        return "Zaten bir hesap var; giriş yapın.";
      case 400:
        return `Geçersiz değer: ${err.message}`;
    }
  }
  return "Backend'e ulaşılamadı. Biraz sonra tekrar deneyin.";
}

export async function loginAction(_prev: LoginState, formData: FormData): Promise<LoginState> {
  let session;
  try {
    session = await login(String(formData.get("username") ?? "").trim(), String(formData.get("password") ?? ""));
  } catch (err) {
    return { error: refusal(err) };
  }
  await setSessionCookie(session.token, session.expires_at);
  redirect(safeNext(formData.get("next")));
}

/** Creates the first administrator and signs them in. */
export async function setupAction(_prev: LoginState, formData: FormData): Promise<LoginState> {
  const password = String(formData.get("password") ?? "");
  if (password !== String(formData.get("password_again") ?? "")) {
    return { error: "Parolalar birbirini tutmuyor." };
  }
  let session;
  try {
    session = await setup({
      username: String(formData.get("username") ?? "").trim(),
      display_name: String(formData.get("display_name") ?? "").trim(),
      password,
    });
  } catch (err) {
    return { error: refusal(err) };
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
