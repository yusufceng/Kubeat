import "server-only";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { connection } from "next/server";
import { loginEnabled, SESSION_COOKIE, validSession } from "@/lib/session";

/**
 * Sends the visitor to the sign-in page unless they have a valid session.
 * Called before every backend request, so no page or action can reach the
 * backend without one, whatever route it is on.
 */
export async function requireSession(): Promise<void> {
  if (!loginEnabled) return;
  // Whether a session has expired depends on when the request is made, so
  // this is never part of anything rendered ahead of time.
  await connection();
  const value = (await cookies()).get(SESSION_COOKIE)?.value;
  if (!validSession(value)) redirect("/login");
}
