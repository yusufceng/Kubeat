"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Suspense } from "react";
import { PulseIcon } from "@/app/ui/icons";

const links = [
  {
    href: "/",
    label: "Panel",
    match: (p: string) =>
      p === "/" || p.startsWith("/monitors") || p.startsWith("/namespaces"),
  },
  {
    href: "/discovery",
    label: "Keşif",
    match: (p: string) => p.startsWith("/discovery"),
  },
  {
    href: "/incidents",
    label: "Olaylar",
    match: (p: string) => p.startsWith("/incidents"),
  },
  {
    href: "/maintenance",
    label: "Bakım",
    match: (p: string) => p.startsWith("/maintenance"),
  },
  {
    href: "/channels",
    label: "Bildirimler",
    match: (p: string) => p.startsWith("/channels"),
  },
  {
    href: "/status-pages",
    label: "Durum sayfaları",
    match: (p: string) => p.startsWith("/status-pages"),
  },
];

/**
 * The pathname is request data, so on prerendered dynamic routes the bar is
 * first sent without a highlighted link and filled in when it streams.
 */
export function Nav({ children, tail }: { children?: React.ReactNode; tail?: React.ReactNode }) {
  return (
    <Suspense fallback={<NavBar pathname={null} tail={tail}>{children}</NavBar>}>
      <ActiveNav tail={tail}>{children}</ActiveNav>
    </Suspense>
  );
}

function ActiveNav({ children, tail }: { children?: React.ReactNode; tail?: React.ReactNode }) {
  return <NavBar pathname={usePathname()} tail={tail}>{children}</NavBar>;
}

/**
 * children sit at the far end of the bar (theme and session controls);
 * tail comes after the links (what only some users have).
 */
function NavBar({ pathname, children, tail }: { pathname: string | null; children?: React.ReactNode; tail?: React.ReactNode }) {
  // The sign-in pages and the public status pages show the name only: their
  // visitors can reach nothing else.
  const bare = pathname === "/login" || pathname === "/setup" || (pathname?.startsWith("/status/") ?? false);
  return (
    <header className="shrink-0 border-b border-line bg-panel">
      <nav className="flex h-12 items-center gap-2 px-4">
        <Link
          href={bare ? (pathname ?? "/") : "/"}
          className="mr-3 flex items-center gap-2 text-base font-semibold tracking-tight"
        >
          <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-accent text-accent-ink">
            <PulseIcon />
          </span>
          Kubeat
        </Link>
        <ul className="flex gap-1 overflow-x-auto text-sm">
          {(bare ? [] : links).map((link) => {
            const active = pathname !== null && link.match(pathname);
            return (
              <li key={link.href}>
                <Link
                  href={link.href}
                  aria-current={active ? "page" : undefined}
                  className={`block rounded-lg px-3 py-1.5 ${
                    active ? "bg-sunken font-medium text-ink" : "text-muted hover:text-ink"
                  }`}
                >
                  {link.label}
                </Link>
              </li>
            );
          })}
          {!bare && tail}
        </ul>
        <div className="ml-auto flex items-center gap-1">{children}</div>
      </nav>
    </header>
  );
}
