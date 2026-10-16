import "server-only";

import { cookies, headers } from "next/headers";
import { config } from "@/lib/config";
import { forwardedHttps, isHttpsUrl } from "@/lib/security-headers";

/**
 * The browser's session: a cookie holding the token the backend issued at
 * sign-in. The backend knows whether it is still good; this side only
 * carries it.
 */
export const SESSION_COOKIE = "kubeat_session";

/**
 * The state of an OIDC sign-in under way, kept by the browser that started
 * it. The callback goes on only with the state this browser left with, so
 * that nobody can finish a sign-in of their own in someone else's browser
 * and have them work, unknowingly, in the wrong account.
 */
const OIDC_STATE_COOKIE = "kubeat_oidc_state";
/** Only the sign-in routes need the state. */
const OIDC_STATE_PATH = "/auth";
/**
 * As long as the backend waits for a sign-in to come back (oidcauth's
 * StateTTL); a later callback fails there in any case.
 */
const OIDC_STATE_MAX_AGE = 10 * 60;

/** The session token of the request, or null without one. */
export async function sessionToken(): Promise<string | null> {
  return (await cookies()).get(SESSION_COOKIE)?.value ?? null;
}

/**
 * Whether cookies are marked Secure, so that the browser never sends them
 * over plain HTTP: as COOKIE_SECURE says, or, left at "auto", when users
 * reach us at an HTTPS address or the proxies in front of us say the
 * request came over HTTPS. It leans towards Secure: wrongly set, it only
 * makes the asking browser drop the cookie.
 */
async function secureCookies(): Promise<boolean> {
  if (config.cookieSecure !== "auto") return config.cookieSecure === "always";
  return isHttpsUrl(config.publicUrl) || forwardedHttps((await headers()).get("x-forwarded-proto"));
}

/** Keeps the token in the browser until the session expires. */
export async function setSessionCookie(token: string, expiresAt: string): Promise<void> {
  const secure = await secureCookies();
  (await cookies()).set(SESSION_COOKIE, token, {
    httpOnly: true,
    sameSite: "lax",
    secure,
    path: "/",
    expires: new Date(expiresAt),
  });
}

export async function clearSessionCookie(): Promise<void> {
  (await cookies()).delete(SESSION_COOKIE);
}

/**
 * Remembers the state of the sign-in this browser is leaving for. Lax, not
 * Strict: the provider sends the browser back with a cross-site navigation.
 */
export async function setOidcState(state: string): Promise<void> {
  const secure = await secureCookies();
  (await cookies()).set(OIDC_STATE_COOKIE, state, {
    httpOnly: true,
    sameSite: "lax",
    secure,
    path: OIDC_STATE_PATH,
    maxAge: OIDC_STATE_MAX_AGE,
  });
}

/**
 * The state of the sign-in this browser started, or null. It is forgotten
 * as it is read: a state is good once, whatever comes of the callback.
 */
export async function takeOidcState(): Promise<string | null> {
  const jar = await cookies();
  const state = jar.get(OIDC_STATE_COOKIE)?.value ?? null;
  if (state !== null) jar.delete({ name: OIDC_STATE_COOKIE, path: OIDC_STATE_PATH });
  return state;
}
