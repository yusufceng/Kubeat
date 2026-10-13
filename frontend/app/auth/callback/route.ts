import type { NextRequest } from "next/server";
import { config } from "@/lib/config";
import { setSessionCookie } from "@/lib/session";

/**
 * Where the identity provider sends the browser back to. The code and
 * state go to the backend, which exchanges and verifies them and answers
 * with a session; the cookie is set here as after a password sign-in.
 */
export async function GET(request: NextRequest) {
  const params = request.nextUrl.searchParams;
  const back = (reason: string) => Response.redirect(new URL(`/login?oidc=${reason}`, request.url), 303);
  if (params.get("error")) return back("refused");
  const state = params.get("state");
  const code = params.get("code");
  if (!state || !code) return back("refused");

  let res: Response;
  try {
    res = await fetch(`${config.backendUrl}/api/v1/auth/oidc/callback`, {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ state, code }),
      signal: AbortSignal.timeout(config.backendTimeoutMs),
    });
  } catch {
    return back("unavailable");
  }
  if (!res.ok) {
    switch (res.status) {
      case 403:
        return back("norole");
      case 502:
        return back("unavailable");
      default:
        return back("refused");
    }
  }
  const session = (await res.json()) as { token: string; expires_at: string };
  await setSessionCookie(session.token, session.expires_at);
  return Response.redirect(new URL("/", request.url), 303);
}
