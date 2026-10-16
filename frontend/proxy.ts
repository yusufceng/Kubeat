import { type NextRequest, NextResponse } from "next/server";
import { config as settings } from "@/lib/config";
import { forwardedHttps, isHttpsUrl, securityHeaders } from "@/lib/security-headers";
import { SESSION_COOKIE } from "@/lib/session";

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
  const response = route(request);
  const headers = securityHeaders(request.nextUrl.pathname, {
    dev: process.env.NODE_ENV === "development",
    https: forwardedHttps(request.headers.get("x-forwarded-proto")),
    publicHttps: isHttpsUrl(settings.publicUrl),
    statusPageFrameAncestors: settings.statusPageFrameAncestors,
    hstsMaxAge: settings.hstsMaxAge,
  });
  for (const [name, value] of Object.entries(headers)) response.headers.set(name, value);
  return response;
}

/** Where the request goes: on to what it asked for, or to the sign-in page. */
function route(request: NextRequest): NextResponse {
  const { pathname } = request.nextUrl;
  // Reports of push monitors come from jobs, which have no session; the
  // token in their path is checked by the backend. Status pages and their
  // badges are public by design.
  // The OIDC relay (/auth/oidc, /auth/callback) runs before any session exists.
  if (pathname.startsWith("/api/push/") || pathname.startsWith("/status/") || pathname.startsWith("/api/badge/") || pathname.startsWith("/auth/")) {
    return NextResponse.next();
  }
  const signedIn = Boolean(request.cookies.get(SESSION_COOKIE)?.value);
  if (pathname === "/login" || pathname === "/setup") {
    if (pathname === "/login" && signedIn) {
      if (request.nextUrl.searchParams.has("expired")) {
        // The backend no longer accepts the session (lib/auth.ts sent the
        // visitor here): drop the cookie, or they would bounce between
        // here and the page that rejected it.
        const response = NextResponse.next();
        response.cookies.delete(SESSION_COOKIE);
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
