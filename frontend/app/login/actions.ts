"use server";

import { cookies, headers } from "next/headers";
import { redirect } from "next/navigation";
import {
  checkPassword,
  createSession,
  lockedForMs,
  SESSION_COOKIE,
  sessionTtlMs,
} from "@/lib/session";

export type LoginState = { error: string | null };

/** Only same-site paths are followed after sign-in. */
function safeNext(value: FormDataEntryValue | null): string {
  const next = typeof value === "string" ? value : "";
  return next.startsWith("/") && !next.startsWith("//") && !next.startsWith("/\\") ? next : "/";
}

export async function loginAction(_prev: LoginState, formData: FormData): Promise<LoginState> {
  const locked = lockedForMs();
  if (locked > 0) {
    return {
      error: `Çok fazla hatalı deneme yapıldı. ${Math.ceil(locked / 60_000)} dakika sonra tekrar deneyin.`,
    };
  }
  if (!checkPassword(String(formData.get("password") ?? ""))) {
    return { error: "Parola hatalı." };
  }

  // Marked Secure when the browser reached us over HTTPS, as it does through
  // a Route or Ingress; plain HTTP is only expected on localhost.
  const https = (await headers()).get("x-forwarded-proto") === "https";
  (await cookies()).set(SESSION_COOKIE, createSession(), {
    httpOnly: true,
    sameSite: "lax",
    secure: https,
    path: "/",
    maxAge: Math.floor(sessionTtlMs / 1000),
  });
  redirect(safeNext(formData.get("next")));
}

export async function logoutAction(): Promise<void> {
  (await cookies()).delete(SESSION_COOKIE);
  redirect("/login");
}
