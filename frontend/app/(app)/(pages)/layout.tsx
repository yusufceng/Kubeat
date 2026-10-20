import { MainColumn } from "@/app/ui/main-column";

/**
 * The sections that are a page alone (incidents, maintenance, status pages,
 * reports, discovery, notifications, users, the audit log, the account):
 * each renders its own <main className="page">, or "page page-wide" for a
 * table that wants the full width.
 */
export default function PagesLayout({ children }: { children: React.ReactNode }) {
  return <MainColumn>{children}</MainColumn>;
}
