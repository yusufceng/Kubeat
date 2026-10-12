import "server-only";

import { redirect } from "next/navigation";
import { connection } from "next/server";
import { cache } from "react";
import { ApiError, getMe, type Me } from "@/lib/api";
import { sessionToken } from "@/lib/session";

/**
 * Who is signed in, asked of the backend once per request. Without a
 * session, or with one the backend no longer accepts, the visitor is sent
 * to the sign-in page.
 */
export const currentUser = cache(async (): Promise<Me> => {
  // Whether a session is still good depends on when the request is made.
  await connection();
  if (!(await sessionToken())) redirect("/login");
  try {
    return await getMe();
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) redirect("/login?expired=1");
    throw err;
  }
});

/** Sends the visitor to the sign-in page unless they have a session. */
export async function requireSession(): Promise<void> {
  await connection();
  if (!(await sessionToken())) redirect("/login");
}
