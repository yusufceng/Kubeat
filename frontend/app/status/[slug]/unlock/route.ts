import type { NextRequest } from "next/server";
import { seeOther } from "@/app/auth/see-other";
import { ApiError, unlockStatusPage } from "@/lib/api";
import { keepStatusPageToken, statusPagePath } from "@/app/status/[slug]/access";

/** Why an unlock did not open the page; the page shows each in words (?unlock=…). */
export type UnlockProblem = "wrong" | "throttled" | "unavailable";

/**
 * Where the visitor goes after the unlock: the page they were on, as long
 * as it is this page or one of its detail pages. Anything else, and the
 * page itself.
 */
function destination(next: string, slug: string): string {
  const page = statusPagePath(slug);
  return next === page || next.startsWith(`${page}/`) ? next : page;
}

/**
 * The unlock form of a protected status page posts here, with or without
 * JavaScript. The password goes to the backend, which answers with a
 * token; it is kept in an HttpOnly cookie scoped to the page, and the
 * browser is sent back to the page, which now opens. A refused password
 * sends it back with the reason in the query.
 */
export async function POST(request: NextRequest, ctx: RouteContext<"/status/[slug]/unlock">) {
  const { slug } = await ctx.params;
  const form = await request.formData();
  const password = String(form.get("password") ?? "");
  const next = destination(String(form.get("next") ?? ""), slug);
  const back = (problem: UnlockProblem) => seeOther(`${next}${next.includes("?") ? "&" : "?"}unlock=${problem}`);

  if (!password) return back("wrong");
  try {
    const session = await unlockStatusPage(slug, password);
    await keepStatusPageToken(slug, session.token, session.expires_at);
  } catch (err) {
    if (err instanceof ApiError) {
      if (err.status === 401) return back("wrong");
      if (err.status === 429) return back("throttled");
      // No password on the page any more (400), or no such page (404): the page itself says what is the case.
      if (err.status === 400 || err.status === 404) return seeOther(next);
    }
    return back("unavailable");
  }
  return seeOther(next);
}
