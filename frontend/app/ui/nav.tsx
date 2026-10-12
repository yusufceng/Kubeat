"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Suspense } from "react";

const links = [
  { href: "/", label: "Monitor'lar", match: (p: string) => p === "/" || p.startsWith("/monitors") },
  { href: "/incidents", label: "Olaylar", match: (p: string) => p.startsWith("/incidents") },
  { href: "/channels", label: "Bildirimler", match: (p: string) => p.startsWith("/channels") },
];

/**
 * The pathname is request data, so on prerendered dynamic routes the bar is
 * first sent without a highlighted link and filled in when it streams.
 */
export function Nav() {
  return (
    <Suspense fallback={<NavBar pathname={null} />}>
      <ActiveNav />
    </Suspense>
  );
}

function ActiveNav() {
  return <NavBar pathname={usePathname()} />;
}

function NavBar({ pathname }: { pathname: string | null }) {
  return (
    <header className="border-b border-black/10 dark:border-white/15">
      <nav className="mx-auto flex w-full max-w-5xl items-center gap-6 px-4 py-3">
        <Link href="/" className="text-base font-semibold tracking-tight">
          Kubeat
        </Link>
        <ul className="flex gap-1 text-sm">
          {links.map((link) => {
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
      </nav>
    </header>
  );
}
