import { type NextRequest, NextResponse } from "next/server";
import { config as settings } from "@/lib/config";
import { sessionCookie, signOutCookies } from "@/lib/cookie-names";
import { forwardedHttps, securityHeaders } from "@/lib/security-headers";
import { secureCookiesFor } from "@/lib/session";

/**
 * Sends visitors without a session cookie to the sign-in page before any
 * page renders. Whether the cookie is still good is the backend's call,
 * made where the data is read (lib/auth.ts).
 *
 * Every answer that passes through here, a redirect as much as a page,
 * also gets the headers of lib/security-headers.ts: the matcher leaves out
 * only Next.js's own static files.
 */
export function proxy(request: NextRequest) {
  const forwardedProto = request.headers.get("x-forwarded-proto");
  const response = route(request, secureCookiesFor(forwardedProto));
  const headers = securityHeaders(request.nextUrl.pathname, {
    dev: process.env.NODE_ENV === "development",
    https: forwardedHttps(forwardedProto),
    publicUrl: settings.publicUrl,
    cookieSecure: settings.cookieSecure,
    statusPageFrameAncestors: settings.statusPageFrameAncestors,
    hstsMaxAge: settings.hstsMaxAge,
  });
  for (const [name, value] of Object.entries(headers)) response.headers.set(name, value);
  return response;
}

/**
 * Where the request goes: on to what it asked for, or to the sign-in page.
 * secure says whether cookies are Secure, which names the session cookie
 * (lib/cookie-names.ts).
 */
function route(request: NextRequest, secure: boolean): NextResponse {
  const { pathname } = request.nextUrl;
  // Reports of push monitors come from jobs, which have no session; the
  // token in their path is checked by the backend. Status pages and their
  // badges are public by design.
  // The OIDC relay (/auth/oidc, /auth/callback) runs before any session exists.
  if (pathname.startsWith("/api/push/") || pathname.startsWith("/status/") || pathname.startsWith("/api/badge/") || pathname.startsWith("/auth/")) {
    return NextResponse.next();
  }
  // The icons of the tab and the home screen (app/icon.svg,
  // app/apple-icon.png) are the same for everyone, on the sign-in page and
  // on a public status page too.
  if (pathname === "/icon.svg" || pathname === "/apple-icon.png") return NextResponse.next();
  const session = sessionCookie(secure);
  const signedIn = Boolean(request.cookies.get(session)?.value);
  if (pathname === "/login" || pathname === "/setup") {
    if (pathname === "/login" && signedIn) {
      if (request.nextUrl.searchParams.has("expired")) {
        // The backend no longer accepts the session (lib/auth.ts sent the
        // visitor here): drop the cookie, or they would bounce between
        // here and the page that rejected it; over HTTPS the plain names
        // go too, as on signing out.
        const response = NextResponse.next();
        for (const cookie of signOutCookies(secure)) response.cookies.delete(cookie);
        return response;
      }
      return NextResponse.redirect(new URL("/", request.url));
    }
    return NextResponse.next();
  }
  if (signedIn) return NextResponse.next();

  const login = new URL("/login", request.url);
  if (pathname !== "/") login.searchParams.set("next", pathname + request.nextUrl.search);
  return NextResponse.redirect(login);
}

export const config = {
  // Everything except Next.js's own static files.
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
