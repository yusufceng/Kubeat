import type { NextRequest } from "next/server";
import { config } from "@/lib/config";
import { incidentQueryParams } from "@/lib/api";
import { filterToQuery, parseIncidentFilter } from "@/lib/incident-filter";
import { sessionToken } from "@/lib/session";

/**
 * The incident list as a CSV download, fetched with the browser's session:
 * every incident the list's filter matches, on all its pages, in its order.
 * The filter is read as the list reads it, so the file holds what the
 * page shows.
 */
export async function GET(request: NextRequest) {
  const token = await sessionToken();
  if (!token) return Response.json({ error: "no session" }, { status: 401 });
  const filter = parseIncidentFilter(request.nextUrl.searchParams, config.incidentRanges);
  const params = incidentQueryParams(filterToQuery(filter, Date.now()));
  params.set("format", "csv");
  try {
    const res = await fetch(`${config.backendUrl}/api/v1/incidents?${params}`, {
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
