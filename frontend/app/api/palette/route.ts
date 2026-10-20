import { ApiError, listMonitors } from "@/lib/api";
import { kindShort } from "@/lib/kinds";
import type { PaletteMonitor } from "@/app/ui/command-palette";
import { monitorLabel } from "@/app/ui/monitor-name";
import { displayState } from "@/app/ui/status";

/**
 * Every monitor the viewer may see, in brief, for the command palette. It
 * is read when the palette opens, with the browser's session, so that the
 * pages do not carry the list on every refresh.
 */
export async function GET() {
  const headers = { "Cache-Control": "no-store" };
  try {
    const monitors = await listMonitors();
    const brief: PaletteMonitor[] = monitors.map((m) => ({
      id: m.id,
      name: monitorLabel(m).name,
      kind: kindShort(m.source, m.type, m.target),
      namespace: m.namespace,
      state: displayState(m),
      target: m.target,
    }));
    return Response.json(brief, { headers });
  } catch (err) {
    const status = err instanceof ApiError && err.status === 401 ? 401 : 502;
    return Response.json({ error: status === 401 ? "no session" : "the backend is not reachable" }, { status, headers });
  }
}
