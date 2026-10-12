"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Suspense } from "react";

const links = [
  {
    href: "/",
    label: "Monitor'lar",
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

/** children sit at the far end of the bar (the session controls). */
function NavBar({ pathname, children }: { pathname: string | null; children?: React.ReactNode }) {
  // The sign-in page shows the name only: nothing else is reachable yet.
  const signingIn = pathname === "/login";
  return (
    <header className="border-b border-black/10 dark:border-white/15">
      <nav className="mx-auto flex w-full max-w-5xl items-center gap-6 px-4 py-3">
        <Link href="/" className="text-base font-semibold tracking-tight">
          Kubeat
        </Link>
        <ul className="flex gap-1 text-sm">
          {(signingIn ? [] : links).map((link) => {
            const active = pathname !== null && link.match(pathname);
            return (
              <li key={link.href}>
                <Link
                  href={link.href}
                  aria-current={active ? "page" : undefined}
                  className={`rounded-md px-3 py-1.5 ${
                    active
                      ? "bg-black/5 font-medium dark:bg-white/10"
                      : "text-zinc-500 hover:text-inherit"
                  }`}
                >
                  {link.label}
                </Link>
              </li>
            );
          })}
        </ul>
        <div className="ml-auto">{children}</div>
      </nav>
    </header>
  );
}
