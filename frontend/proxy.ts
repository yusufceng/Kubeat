import { type NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE } from "@/lib/session";

/**
 * Sends visitors without a session cookie to the sign-in page before any
 * page renders. Whether the cookie is still good is the backend's call,
 * made where the data is read (lib/auth.ts).
 */
export function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;
  // Reports of push monitors come from jobs, which have no session; the
  // token in their path is checked by the backend. Status pages and their
  // badges are public by design.
  if (pathname.startsWith("/api/push/") || pathname.startsWith("/status/") || pathname.startsWith("/api/badge/")) {
    return NextResponse.next();
  }
  const signedIn = Boolean(request.cookies.get(SESSION_COOKIE)?.value);
  if (pathname === "/login" || pathname === "/setup") {
    return signedIn && pathname === "/login" ? NextResponse.redirect(new URL("/", request.url)) : NextResponse.next();
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
