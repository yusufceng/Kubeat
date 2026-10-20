import type { NextRequest } from "next/server";
import { config } from "@/lib/config";
import { sessionToken } from "@/lib/session";

/** The report as a CSV download, fetched with the browser's session. */
export async function GET(request: NextRequest) {
  const token = await sessionToken();
  if (!token) return Response.json({ error: "no session" }, { status: 401 });
  const params = new URLSearchParams(request.nextUrl.searchParams);
  params.set("format", "csv");
  try {
    const res = await fetch(`${config.backendUrl}/api/v1/reports?${params}`, {
      cache: "no-store",
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(config.backendTimeoutMs),
    });
    const headers = new Headers({ "Content-Type": res.headers.get("Content-Type") ?? "application/json" });
    const disposition = res.headers.get("Content-Disposition");
    if (disposition) headers.set("Content-Disposition", disposition);
    return new Response(await res.text(), { status: res.status, headers });
  } catch {
    return Response.json({ error: "the backend is not reachable" }, { status: 502 });
  }
}
