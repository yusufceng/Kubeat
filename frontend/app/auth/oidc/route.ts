import type { NextRequest } from "next/server";
import { seeOther } from "@/app/auth/see-other";
import { config } from "@/lib/config";
import { setOidcState } from "@/lib/session";

/**
 * Sends the browser to the identity provider. The backend is the OIDC
 * client and holds the state; this relays the URL it chose, and keeps the
 * state in this browser for the callback to compare (lib/session.ts).
 */
export async function GET(request: NextRequest) {
  const unavailable = () => seeOther("/login?oidc=unavailable");
  let url: string;
  try {
    // Where this server thinks it is served. The backend uses it only on
    // a developer's machine (next dev on localhost) and without a redirect
    // URL of its own; anywhere else that comes from its configuration.
    const callback = new URL("/auth/callback", request.url).toString();
    const res = await fetch(`${config.backendUrl}/api/v1/auth/oidc/start?redirect=${encodeURIComponent(callback)}`, {
      cache: "no-store",
      signal: AbortSignal.timeout(config.backendTimeoutMs),
    });
    if (!res.ok) return unavailable();
    ({ url } = (await res.json()) as { url: string });
  } catch {
    return unavailable();
  }
  const state = stateOf(url);
  if (!state) return unavailable();
  await setOidcState(state);
  return seeOther(url);
}

/** The state in the provider URL the backend made, or null. */
function stateOf(url: string): string | null {
  try {
    return new URL(url).searchParams.get("state") || null;
  } catch {
    return null;
  }
}
