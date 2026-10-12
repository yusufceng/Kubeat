import "server-only";

import { cookies, headers } from "next/headers";

/**
 * The browser's session: a cookie holding the token the backend issued at
 * sign-in. The backend knows whether it is still good; this side only
 * carries it.
 */
export const SESSION_COOKIE = "kubeat_session";

/** The session token of the request, or null without one. */
export async function sessionToken(): Promise<string | null> {
  return (await cookies()).get(SESSION_COOKIE)?.value ?? null;
}

/** Keeps the token in the browser until the session expires. */
export async function setSessionCookie(token: string, expiresAt: string): Promise<void> {
  // Marked Secure when the browser reached us over HTTPS, as it does through
  // a Route or Ingress; plain HTTP is only expected on localhost.
  const https = (await headers()).get("x-forwarded-proto") === "https";
  (await cookies()).set(SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: "lax",
    secure: https,
    path: "/",
    expires: new Date(expiresAt),
  });
}

export async function clearSessionCookie(): Promise<void> {
  (await cookies()).delete(SESSION_COOKIE);
}
