import { type NextRequest, NextResponse } from "next/server";
import { loginEnabled, SESSION_COOKIE, validSession } from "@/lib/session";

/**
 * Redirects visitors without a session to the sign-in page before any page
 * renders. This is the early check; lib/auth.ts repeats it where data is
 * read and changed.
 */
export function proxy(request: NextRequest) {
  if (!loginEnabled) return NextResponse.next();

  const { pathname } = request.nextUrl;
  // Reports of push monitors come from jobs, which have no session; the
  // token in their path is checked by the backend.
  if (pathname.startsWith("/api/push/")) return NextResponse.next();

  const signedIn = validSession(request.cookies.get(SESSION_COOKIE)?.value);

  if (pathname === "/login") {
    return signedIn ? NextResponse.redirect(new URL("/", request.url)) : NextResponse.next();
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
