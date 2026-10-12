import type { NextRequest } from "next/server";
import { config } from "@/lib/config";

/**
 * Passes the report of a push monitor's source on to the backend, so that
 * jobs can report to the one address everyone can reach: this application's.
 * No session is needed; the token in the path is the credential, and the
 * backend is the one that checks it.
 */
async function forward(request: NextRequest, ctx: RouteContext<"/api/push/[token]">) {
  const { token } = await ctx.params;
  const target = `${config.backendUrl}/api/push/${encodeURIComponent(token)}${request.nextUrl.search}`;
  try {
    const res = await fetch(target, {
      method: request.method,
      cache: "no-store",
      signal: AbortSignal.timeout(config.backendTimeoutMs),
    });
    return new Response(await res.text(), {
      status: res.status,
      headers: { "Content-Type": res.headers.get("Content-Type") ?? "application/json" },
    });
  } catch {
    return Response.json({ ok: false, error: "the backend is not reachable" }, { status: 502 });
  }
}

export { forward as GET, forward as POST };
