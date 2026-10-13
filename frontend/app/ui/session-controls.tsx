import Link from "next/link";
import { connection } from "next/server";
import { Suspense } from "react";
import { getMe } from "@/lib/api";
import { sessionToken } from "@/lib/session";
import { logoutAction } from "@/app/login/actions";
import { getT } from "@/lib/i18n/server";

/**
 * Who is signed in, with the way to their account and out. Rendered per
 * request: the session is request data.
 */
export function SessionControls() {
  return (
    <Suspense>
      <Controls />
    </Suspense>
  );
}

async function Controls() {
  await connection();
  if (!(await sessionToken())) return null;
  let me;
  try {
    me = await getMe();
  } catch {
    return null; // the page itself will say what is wrong
  }
  const t = await getT();
  const role = t(`role.${me.role}`);
  return (
    <div className="flex items-center gap-1">
      <Link
        href="/account"
        className="hidden rounded-lg px-2 py-1.5 text-sm text-muted hover:text-ink sm:block"
        title={t("session.account", { name: me.username, role })}
      >
        {me.display_name || me.username}
        <span className="ml-1.5 rounded bg-sunken px-1.5 py-0.5 text-xs">{role}</span>
      </Link>
      <form action={logoutAction}>
        <button type="submit" className="rounded-lg px-3 py-1.5 text-sm text-muted hover:text-ink">
          {t("session.signOut")}
        </button>
      </form>
    </div>
  );
}

/** The links only administrators have, in the top bar. */
export function AdminNav() {
  return (
    <Suspense>
      <AdminLinks />
    </Suspense>
  );
}

async function AdminLinks() {
  await connection();
  if (!(await sessionToken())) return null;
  let me;
  try {
    me = await getMe();
  } catch {
    return null;
  }
  if (!me.admin) return null;
  const t = await getT();
  return (
    <>
      <li>
        <Link href="/users" className="block rounded-lg px-3 py-1.5 text-muted hover:text-ink">
          {t("nav.users")}
        </Link>
      </li>
      <li>
        <Link href="/audit" className="block rounded-lg px-3 py-1.5 text-muted hover:text-ink">
          {t("nav.audit")}
        </Link>
      </li>
    </>
  );
}
