import { unstable_rethrow } from "next/navigation";
import { cache, Suspense } from "react";
import type { Me } from "@/lib/api";
import { currentUser } from "@/lib/auth";
import { getT } from "@/lib/i18n/server";
import { ShellNav } from "@/app/ui/nav";

/**
 * Who is signed in, for the frame. Without a session the visitor is sent to
 * the sign-in page, as by currentUser(); when the backend cannot answer, the
 * frame goes without the user's parts and the page says what is wrong.
 */
export const shellUser = cache(async (): Promise<Me | null> => {
  try {
    return await currentUser();
  } catch (err) {
    unstable_rethrow(err);
    return null;
  }
});

/**
 * The navigation with the signed-in user's parts: the administrators'
 * sections and the account menu. Rendered per request, since the session is
 * request data; until it is known the navigation shows without them.
 */
export function SessionNav() {
  return (
    <Suspense fallback={<ShellNav user={null} />}>
      <NavForUser />
    </Suspense>
  );
}

async function NavForUser() {
  const me = await shellUser();
  const t = await getT();
  return (
    <ShellNav
      user={
        me && {
          name: me.display_name || me.username,
          username: me.username,
          role: t(`role.${me.role}`),
          admin: me.admin,
        }
      }
    />
  );
}
