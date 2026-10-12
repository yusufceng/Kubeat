import { Suspense } from "react";
import { getHealth, type Health, REQUIRED_API_REVISION } from "@/lib/api";

/**
 * Says so when the backend is older than this frontend expects, which
 * happens when only one of the two was restarted or rolled out.
 */
export function BackendWarning() {
  return (
    <Suspense>
      <RevisionCheck />
    </Suspense>
  );
}

async function RevisionCheck() {
  let health: Health;
  try {
    health = await getHealth();
  } catch {
    return null; // an unreachable backend is reported by the page itself
  }
  if ((health.api_revision ?? 0) >= REQUIRED_API_REVISION) return null;

  return (
    <p
      role="alert"
      className="border-b border-amber-600/40 bg-amber-500/10 px-4 py-2 text-center text-sm text-amber-800 dark:text-amber-300"
    >
      Backend bu arayüzden eski bir sürüm çalıştırıyor; bazı bilgiler eksik görünebilir.
      Backend&apos;i yeniden başlatın ya da güncelleyin.
    </p>
  );
}
