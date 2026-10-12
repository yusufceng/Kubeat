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
];

/**
 * The pathname is request data, so on prerendered dynamic routes the bar is
 * first sent without a highlighted link and filled in when it streams.
 */
export function Nav({ children }: { children?: React.ReactNode }) {
  return (
    <Suspense fallback={<NavBar pathname={null}>{children}</NavBar>}>
      <ActiveNav>{children}</ActiveNav>
    </Suspense>
  );
}

function ActiveNav({ children }: { children?: React.ReactNode }) {
  return <NavBar pathname={usePathname()}>{children}</NavBar>;
}

/** children sit at the far end of the bar (theme and session controls). */
function NavBar({ pathname, children }: { pathname: string | null; children?: React.ReactNode }) {
  // The sign-in page shows the name only: nothing else is reachable yet.
  const signingIn = pathname === "/login";
  return (
    <header className="shrink-0 border-b border-line bg-panel">
      <nav className="flex h-12 items-center gap-2 px-4">
        <Link href="/" className="mr-3 flex items-center gap-2 text-base font-semibold tracking-tight">
          <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-accent text-accent-ink">
            <PulseIcon />
          </span>
          Kubeat
        </Link>
        <ul className="flex gap-1 overflow-x-auto text-sm">
          {(signingIn ? [] : links).map((link) => {
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
        </ul>
        <div className="ml-auto flex items-center gap-1">{children}</div>
      </nav>
    </header>
  );
}
