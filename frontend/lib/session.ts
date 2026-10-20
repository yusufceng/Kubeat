import "server-only";

import { cookies, headers } from "next/headers";
import { config } from "@/lib/config";
import { oidcStateCookie, sessionCookie, signOutCookies } from "@/lib/cookie-names";
import { forwardedHttps, isHttpsUrl } from "@/lib/security-headers";

/**
 * As long as the backend waits for a sign-in to come back (oidcauth's
 * StateTTL); a later callback fails there in any case.
 */
const OIDC_STATE_MAX_AGE = 10 * 60;

/**
 * Whether cookies are marked Secure, so that the browser never sends them
 * over plain HTTP: as COOKIE_SECURE says, or, left at "auto", when users
 * reach us at an HTTPS address or the proxies in front of us say the
 * request came over HTTPS (forwardedProto is its X-Forwarded-Proto). It
 * leans towards Secure: wrongly set, it only makes the asking browser drop
 * the cookie.
 */
export function secureCookiesFor(forwardedProto: string | null): boolean {
  if (config.cookieSecure !== "auto") return config.cookieSecure === "always";
  return isHttpsUrl(config.publicUrl) || forwardedHttps(forwardedProto);
}

/** secureCookiesFor the request being answered. */
async function secureCookies(): Promise<boolean> {
  return secureCookiesFor((await headers()).get("x-forwarded-proto"));
}

/** The session token of the request, or null without one. */
export async function sessionToken(): Promise<string | null> {
  return (await cookies()).get(sessionCookie(await secureCookies()))?.value ?? null;
}

/** Keeps the token in the browser until the session expires. */
export async function setSessionCookie(token: string, expiresAt: string): Promise<void> {
  const secure = await secureCookies();
  (await cookies()).set(sessionCookie(secure), token, {
    httpOnly: true,
    sameSite: "lax",
    secure,
    path: "/",
    expires: new Date(expiresAt),
  });
}

/** Drops the session cookie, and over HTTPS the plain names (signOutCookies). */
export async function clearSessionCookie(): Promise<void> {
  const secure = await secureCookies();
  const jar = await cookies();
  for (const cookie of signOutCookies(secure)) jar.delete(cookie);
}

/**
 * Remembers the state of the sign-in this browser is leaving for. Lax, not
 * Strict: the provider sends the browser back with a cross-site navigation.
 * Path=/, which the __Host- prefix requires, though only /auth/callback
 * reads it.
 */
export async function setOidcState(state: string): Promise<void> {
  const secure = await secureCookies();
  (await cookies()).set(oidcStateCookie(secure), state, {
    httpOnly: true,
    sameSite: "lax",
    secure,
    path: "/",
    maxAge: OIDC_STATE_MAX_AGE,
  });
}

/**
 * The state of the sign-in this browser started, or null. It is forgotten
 * as it is read: a state is good once, whatever comes of the callback.
 */
export async function takeOidcState(): Promise<string | null> {
  const secure = await secureCookies();
  const name = oidcStateCookie(secure);
  const jar = await cookies();
  const state = jar.get(name)?.value ?? null;
  if (state !== null) jar.delete({ name, path: "/", secure });
  return state;
}
