import type { NextRequest } from "next/server";
import { config } from "@/lib/config";

/**
 * Sends the browser to the identity provider. The backend is the OIDC
 * client and holds the state; this only relays the URL it chose.
 */
export async function GET(request: NextRequest) {
  try {
    const res = await fetch(`${config.backendUrl}/api/v1/auth/oidc/start`, {
      cache: "no-store",
      signal: AbortSignal.timeout(config.backendTimeoutMs),
    });
    if (!res.ok) return Response.redirect(new URL("/login?oidc=unavailable", request.url), 303);
    const { url } = (await res.json()) as { url: string };
    return Response.redirect(url, 303);
  } catch {
    return Response.redirect(new URL("/login?oidc=unavailable", request.url), 303);
  }
}
