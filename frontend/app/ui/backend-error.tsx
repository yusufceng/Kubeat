import Link from "next/link";
import { ApiError } from "@/lib/api";

/**
 * Shown where a page could not get its data from the backend. A rejected
 * API token is called out, since restarting things will not fix it.
 */
export function BackendError({ what, err }: { what: string; err?: unknown }) {
  if (err instanceof ApiError && err.status === 401) {
    return (
      <p role="alert" className="rounded-lg border border-line bg-sunken p-4 text-sm">
        Oturumunuz sona erdi.{" "}
        <Link href="/login?expired=1" className="underline">
          Yeniden giriş yapın
        </Link>
        .
      </p>
    );
  }
  return (
    <p
      role="alert"
      className="rounded-lg border border-down/40 bg-down/8 p-4 text-sm text-down"
    >
      Backend&apos;e ulaşılamıyor. {what} gösterilemiyor.
    </p>
  );
}
