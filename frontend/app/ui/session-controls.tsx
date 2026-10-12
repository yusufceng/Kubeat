import { connection } from "next/server";
import { Suspense } from "react";
import { cookies } from "next/headers";
import { loginEnabled, SESSION_COOKIE, validSession } from "@/lib/session";
import { logoutAction } from "@/app/login/actions";

/**
 * The sign-out button, or a warning when sign-in is switched off. Rendered
 * per request: whether a password is set is known when the server runs, not
 * when the page shell is built.
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
  if (!loginEnabled) {
    return (
      <span
        role="alert"
        className="rounded-md bg-warn/12 px-2 py-1 text-xs text-warn"
        title="ADMIN_PASSWORD ayarlanmadığı için arayüze erişen herkes değişiklik yapabilir."
      >
        Giriş koruması kapalı
      </span>
    );
  }
  if (!validSession((await cookies()).get(SESSION_COOKIE)?.value)) return null;
  return (
    <form action={logoutAction}>
      <button type="submit" className="rounded-md px-3 py-1.5 text-sm text-muted hover:text-ink">
        Çıkış
      </button>
    </form>
  );
}
