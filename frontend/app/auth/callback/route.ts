import type { NextRequest } from "next/server";
import { seeOther } from "@/app/auth/see-other";
import { config } from "@/lib/config";
import { setSessionCookie, takeOidcState } from "@/lib/session";

/**
 * Where the identity provider sends the browser back to. The code and
 * state go to the backend, which exchanges and verifies them and answers
 * with a session; the cookie is set here as after a password sign-in.
 */
export async function GET(request: NextRequest) {
  const params = request.nextUrl.searchParams;
  const back = (reason: string) => seeOther(`/login?oidc=${reason}`);
  // Read first, so that it is cleared whatever happens next.
  const started = await takeOidcState();
  if (params.get("error")) return back("refused");
  const state = params.get("state");
  const code = params.get("code");
  if (!state || !code) return back("refused");
  // Only a sign-in this browser started goes on. A link with someone
  // else's state and code would otherwise sign the browser into their
  // account.
  if (state !== started) return back("state");

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
  return seeOther("/");
}
