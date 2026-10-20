/*
 * The cookies of a session and of an OIDC sign-in under way. Whether they
 * are Secure is lib/session.ts's call; this only names them, and has no
 * imports, so that the tests run it as it is.
 *
 * Whenever cookies are Secure their names carry the __Host- prefix: a
 * browser takes such a cookie only from this very host, over HTTPS, with
 * Path=/ and no Domain. Without it any other host under a shared apps
 * domain (another tenant's Route, or a script hole in one) could plant a
 * session or a sign-in state of its own for the whole domain, and the
 * visitor would work in the planter's account unknowingly. Only the
 * prefixed name is read then: reading the plain one as well would leave
 * that open. Over plain HTTP no prefix can be used, and the plain names
 * stay.
 */

/**
 * The browser's session: a cookie holding the token the backend issued at
 * sign-in. The backend knows whether it is still good; this side only
 * carries it.
 */
export function sessionCookie(secure: boolean): string {
  return secure ? "__Host-kubeat_session" : "kubeat_session";
}

/**
 * The state of an OIDC sign-in under way, kept by the browser that started
 * it. The callback goes on only with the state this browser left with, so
 * that nobody can finish a sign-in of their own in someone else's browser
 * and have them work, unknowingly, in the wrong account.
 */
export function oidcStateCookie(secure: boolean): string {
  return secure ? "__Host-kubeat_oidc_state" : "kubeat_oidc_state";
}

/** A cookie to delete; the browser matches it by name and path. */
export type CookieToDelete = { name: string; path: string; secure: boolean };

/**
 * What signing out deletes: the session cookie (a __Host- one only goes
 * with Secure and Path=/), and while cookies are Secure, the plain names
 * too, at the paths they had before they took the prefix. Those are never
 * read then, but a session cookie from before the upgrade would be read
 * again if the install went back to plain names (COOKIE_SECURE=never)
 * while its session lasts, and a browser that signed out would be signed
 * in again. Deleting one that another host planted does no harm.
 */
export function signOutCookies(secure: boolean): CookieToDelete[] {
  const session = { name: sessionCookie(secure), path: "/", secure };
  if (!secure) return [session];
  return [
    session,
    { name: sessionCookie(false), path: "/", secure },
    { name: oidcStateCookie(false), path: "/auth", secure },
  ];
}
