import type { NextRequest } from "next/server";
import { config } from "@/lib/config";

/**
 * A monitor's badge (an SVG image) for a README or a wiki, served without a
 * session. The backend draws it, and only for monitors that a published
 * status page shows.
 */
export async function GET(request: NextRequest, ctx: RouteContext<"/api/badge/[id]/[kind]">) {
  const { id, kind } = await ctx.params;
  const target = `${config.backendUrl}/api/v1/badges/${encodeURIComponent(id)}/${encodeURIComponent(kind)}${request.nextUrl.search}`;
  try {
    const res = await fetch(target, {
      cache: "no-store",
      signal: AbortSignal.timeout(config.backendTimeoutMs),
    });
    const headers = new Headers({ "Content-Type": res.headers.get("Content-Type") ?? "application/json" });
    const cache = res.headers.get("Cache-Control");
    if (cache) headers.set("Cache-Control", cache);
    return new Response(await res.text(), { status: res.status, headers });
  } catch {
    return Response.json({ error: "the backend is not reachable" }, { status: 502 });
  }
}
