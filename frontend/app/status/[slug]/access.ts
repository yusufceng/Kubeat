import "server-only";

import { cookies, headers } from "next/headers";
import { secureCookiesFor } from "@/lib/session";

/**
 * The token a visitor holds for a protected status page lives in an
 * HttpOnly cookie scoped to that page's path, /status/<slug>, so that it
 * goes along with the page and its detail pages and nowhere else, and no
 * script can read it. One cookie per page: a visitor may hold several.
 */
export function statusPageCookie(slug: string): string {
  // A slug is lowercase letters, digits and hyphens: fit for a cookie name as it is.
  return `kubeat_sp_${slug}`;
}

export function statusPagePath(slug: string): string {
  return `/status/${encodeURIComponent(slug)}`;
}

/** The token the request carries for the page, or undefined without one. */
export async function statusPageToken(slug: string): Promise<string | undefined> {
  return (await cookies()).get(statusPageCookie(slug))?.value || undefined;
}

/**
 * Keeps the token until the backend says it expires. Secure follows the
 * session cookie's rule (lib/session.ts); Lax, so that a link to the page
 * from elsewhere still opens it unlocked.
 */
export async function keepStatusPageToken(slug: string, token: string, expiresAt: string): Promise<void> {
  const secure = secureCookiesFor((await headers()).get("x-forwarded-proto"));
  (await cookies()).set(statusPageCookie(slug), token, {
    httpOnly: true,
    sameSite: "lax",
    secure,
    path: statusPagePath(slug),
    expires: new Date(expiresAt),
  });
}
