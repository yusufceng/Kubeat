"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Suspense, useId, useRef } from "react";
import { logoutAction } from "@/app/login/actions";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import {
  AccountIcon,
  AuditIcon,
  BellIcon,
  ChevronIcon,
  DiscoveryIcon,
  IncidentsIcon,
  MaintenanceIcon,
  MoreIcon,
  OverviewIcon,
  ReportsIcon,
  SignOutIcon,
  StatusPagesIcon,
  UsersIcon,
} from "@/app/ui/icons";
import { LanguageToggle } from "@/app/ui/language-toggle";
import { Logo, LogoMark } from "@/app/ui/logo";
import { type Crumb, usePageCrumbs } from "@/app/ui/shell-state";
import { ThemeToggle } from "@/app/ui/theme-toggle";

/** The groups of the navigation, in the order they are shown. */
export type NavGroup = "monitoring" | "response" | "reporting" | "admin";

export type Section = {
  href: string;
  /** The message key of its name; short is the tab bar's, where that is narrower. */
  label: string;
  short?: string;
  icon: (p: React.SVGProps<SVGSVGElement>) => React.ReactNode;
  match: (pathname: string) => boolean;
  /** The group the wide navigation lists it under. */
  group: NavGroup;
};

/** The groups and the message keys of their names. */
const navGroups: { key: NavGroup; label: string }[] = [
  { key: "monitoring", label: "nav.group.monitoring" },
  { key: "response", label: "nav.group.response" },
  { key: "reporting", label: "nav.group.reporting" },
  { key: "admin", label: "nav.group.admin" },
];

/** Whether a path is in the workspace: the overview, a monitor or a namespace. */
export function inWorkspace(pathname: string): boolean {
  return pathname === "/" || pathname.startsWith("/monitors") || pathname.startsWith("/namespaces");
}

/** The sections of the app, in the order of the rail; the command palette lists them too. */
export const sections: Section[] = [
  { href: "/", label: "nav.overview", icon: OverviewIcon, match: inWorkspace, group: "monitoring" },
  { href: "/incidents", label: "nav.incidents", icon: IncidentsIcon, match: (p) => p.startsWith("/incidents"), group: "response" },
  { href: "/maintenance", label: "nav.maintenance", icon: MaintenanceIcon, match: (p) => p.startsWith("/maintenance"), group: "response" },
  {
    href: "/status-pages",
    label: "nav.statusPages",
    short: "nav.statusPagesShort",
    icon: StatusPagesIcon,
    match: (p) => p.startsWith("/status-pages"),
    group: "reporting",
  },
  { href: "/reports", label: "nav.reports", icon: ReportsIcon, match: (p) => p.startsWith("/reports"), group: "reporting" },
  { href: "/discovery", label: "nav.discovery", icon: DiscoveryIcon, match: (p) => p.startsWith("/discovery"), group: "monitoring" },
  { href: "/channels", label: "nav.channels", icon: BellIcon, match: (p) => p.startsWith("/channels"), group: "response" },
];

/** What only administrators have. */
export const adminSections: Section[] = [
  { href: "/users", label: "nav.users", icon: UsersIcon, match: (p) => p.startsWith("/users"), group: "admin" },
  { href: "/audit", label: "nav.audit", icon: AuditIcon, match: (p) => p.startsWith("/audit"), group: "admin" },
];

/** The sections the phone's tab bar has room for; the rest are under More. */
const TABS = 4;

/** Who is signed in, as the frame shows them. role is already translated. */
export type ShellUser = { name: string; username: string; role: string; admin: boolean };

function initials(name: string, locale: string): string {
  const words = name.trim().split(/\s+/);
  const letters = words.length > 1 ? words[0][0] + words[1][0] : words[0].slice(0, 2);
  return letters.toLocaleUpperCase(locale);
}

/**
 * The app's navigation: the rail on the left of a wide screen, the tab bar
 * at the bottom of a narrow one. The pathname is request data, so on
 * prerendered dynamic routes the navigation is first sent without a
 * highlighted section and filled in when it streams.
 */
export function ShellNav({ user }: { user: ShellUser | null }) {
  return (
    <Suspense fallback={<NavView pathname={null} user={user} />}>
      <ActiveNav user={user} />
    </Suspense>
  );
}

function ActiveNav({ user }: { user: ShellUser | null }) {
  return <NavView pathname={usePathname()} user={user} />;
}

function NavView({ pathname, user }: { pathname: string | null; user: ShellUser | null }) {
  return (
    <>
      <Rail pathname={pathname} user={user} />
      <TabBar pathname={pathname} user={user} />
    </>
  );
}

/**
 * The navigation of a wide screen. From 1280px it is --shell-nav wide, the
 * sections in their groups under small capital names, each an icon and its
 * name; below that it is a --shell-rail column of icons, each naming itself
 * in a tooltip, the groups told apart by a gap. Its foot is the account
 * menu, which also holds the theme and the language; without a user (the
 * backend cannot answer) those two stand in the rail themselves, so that
 * the frame never loses them. Sticky, the rail is a stacking context of its
 * own: z-40 puts it, and so its tooltips, over the page beside it, which
 * comes later and would otherwise paint on top.
 */
function Rail({ pathname, user }: { pathname: string | null; user: ShellUser | null }) {
  const t = useT();
  const link = (s: Section) => (
    <Link
      key={s.href}
      href={s.href}
      aria-current={pathname !== null && s.match(pathname) ? "page" : undefined}
      className="rail-item"
    >
      <s.icon width={18} height={18} />
      <span className="rail-tip">{t(s.label)}</span>
    </Link>
  );
  const all = [...sections, ...(user?.admin ? adminSections : [])];
  const groups = navGroups.map((g) => ({ ...g, items: all.filter((s) => s.group === g.key) })).filter((g) => g.items.length > 0);
  return (
    <nav
      aria-label={t("shell.nav")}
      className="sticky top-0 z-40 hidden h-dvh w-(--shell-rail) flex-none flex-col items-center gap-1 border-r border-line bg-panel py-2 lg:flex xl:w-(--shell-nav) xl:items-stretch xl:px-2 dark:bg-canvas"
    >
      <Link href="/" aria-label={t("shell.home")} className="mb-2 rounded-md xl:mx-2.5 xl:mb-1 xl:flex xl:h-8 xl:items-center">
        <LogoMark size={28} className="xl:hidden" />
        <Logo size={17} className="hidden text-ink xl:block" />
      </Link>
      {groups.map((g, i) => (
        <div key={g.key} className="contents">
          <span className="rail-group type-label hidden xl:block">{t(g.label)}</span>
          {i > 0 && <span className="h-2 xl:hidden" aria-hidden />}
          {g.items.map(link)}
        </div>
      ))}
      <span className="flex-1" aria-hidden />
      {user ? (
        <AccountMenu user={user} />
      ) : (
        <>
          <ThemeToggle />
          <span className="mt-1.5 mb-1">
            <LanguageToggle />
          </span>
        </>
      )}
    </nav>
  );
}

/** The avatar at the foot of the rail, and its menu: the account, the theme, the language and the way out. */
function AccountMenu({ user }: { user: ShellUser }) {
  const t = useT();
  const id = useId();
  const menu = useRef<HTMLDivElement>(null);
  return (
    <>
      <button
        type="button"
        popoverTarget={id}
        aria-label={t("shell.accountMenu", { name: user.name })}
        className="group flex items-center gap-2.5 rounded-full xl:w-full xl:rounded-md xl:px-2 xl:py-1.5 xl:hover:bg-hover"
      >
        <span className="grid size-7 flex-none place-items-center rounded-full border border-line-strong bg-raised text-micro font-semibold text-ink-2 group-hover:border-ink-3 group-hover:text-ink">
          {initials(user.name, t.intl)}
        </span>
        {/* From 1280px the name and the role stand beside the initials. */}
        <span className="hidden min-w-0 flex-col text-left xl:flex">
          <span className="truncate text-dense font-medium text-ink">{user.name}</span>
          <span className="truncate text-micro text-ink-3">{user.role}</span>
        </span>
      </button>
      <div
        id={id}
        ref={menu}
        popover="auto"
        className="menu inset-auto bottom-2 left-[calc(var(--shell-rail)+8px)] w-60 xl:left-[calc(var(--shell-nav)+8px)]"
      >
        <p className="px-2.5 pt-1.5 pb-2">
          <span className="block truncate font-semibold">{user.name}</span>
          <span className="block truncate text-xs text-ink-3">
            {user.username} · {user.role}
          </span>
        </p>
        <hr className="menu-sep" />
        <Link href="/account" className="menu-item" onClick={() => menu.current?.hidePopover()}>
          <AccountIcon />
          {t("shell.account")}
        </Link>
        <hr className="menu-sep" />
        <ThemeToggle variant="menu" />
        <LanguageToggle variant="menu" />
        <hr className="menu-sep" />
        <form action={logoutAction}>
          <button type="submit" className="menu-item">
            <SignOutIcon />
            {t("session.signOut")}
          </button>
        </form>
      </div>
    </>
  );
}

/** The bottom bar of a narrow screen: four sections, and More for the rest. */
function TabBar({ pathname, user }: { pathname: string | null; user: ShellUser | null }) {
  const t = useT();
  const id = useId();
  const menu = useRef<HTMLDivElement>(null);
  const more = [...sections.slice(TABS), ...(user?.admin ? adminSections : [])];
  const moreActive = pathname !== null && (pathname.startsWith("/account") || more.some((s) => s.match(pathname)));
  const close = () => menu.current?.hidePopover();
  return (
    <>
      <nav
        aria-label={t("shell.nav")}
        className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-line bg-panel pb-[env(safe-area-inset-bottom)] lg:hidden"
      >
        {sections.slice(0, TABS).map((s) => (
          <Link
            key={s.href}
            href={s.href}
            aria-current={pathname !== null && s.match(pathname) ? "page" : undefined}
            className="tab"
          >
            <s.icon width={20} height={20} />
            <span className="max-w-full truncate px-1">{t(s.short ?? s.label)}</span>
          </Link>
        ))}
        <button type="button" popoverTarget={id} className="tab" data-active={moreActive || undefined}>
          <MoreIcon width={20} height={20} />
          {t("shell.more")}
        </button>
      </nav>
      <div id={id} ref={menu} popover="auto" className="menu inset-auto right-2 bottom-[calc(70px+env(safe-area-inset-bottom))] w-64">
        {more.map((s) => (
          <Link
            key={s.href}
            href={s.href}
            aria-current={pathname !== null && s.match(pathname) ? "page" : undefined}
            className="menu-item"
            onClick={close}
          >
            <s.icon />
            {t(s.label)}
          </Link>
        ))}
        <hr className="menu-sep" />
        {user && (
          <Link href="/account" className="menu-item" onClick={close}>
            <AccountIcon />
            {t("shell.account")}
            <span className="ml-auto truncate text-xs text-ink-3">{user.name}</span>
          </Link>
        )}
        <ThemeToggle variant="menu" />
        <LanguageToggle variant="menu" />
        {user && (
          <form action={logoutAction}>
            <button type="submit" className="menu-item">
              <SignOutIcon />
              {t("session.signOut")}
            </button>
          </form>
        )}
      </div>
    </>
  );
}

/** The first stop of the keyboard: straight to the page, past the navigation. */
export function SkipLink() {
  const t = useT();
  return (
    <a
      href="#content"
      className="sr-only z-50 rounded-sm bg-raised px-3 py-2 text-sm font-medium focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
    >
      {t("shell.skip")}
    </a>
  );
}

/** The section a path is in, as a breadcrumb of one, for pages that give none. */
function sectionCrumbs(pathname: string, t: T): Crumb[] {
  if (pathname.startsWith("/account")) return [{ label: t("account.title") }];
  const section = [...sections, ...adminSections].find((s) => s.match(pathname));
  if (!section) return [];
  // Below the overview (a monitor, a namespace) the section links back to it.
  return [{ label: t(section.label), href: pathname === section.href ? undefined : section.href }];
}

/**
 * Where the page is, at the left of the context bar: what the page gave
 * with <Crumbs>, or else the name of its section.
 */
export function Breadcrumb() {
  return (
    <Suspense fallback={<CrumbTrail items={[]} />}>
      <ActiveBreadcrumb />
    </Suspense>
  );
}

function ActiveBreadcrumb() {
  const t = useT();
  const pathname = usePathname();
  const page = usePageCrumbs();
  return <CrumbTrail items={page ?? sectionCrumbs(pathname, t)} />;
}

/**
 * The steps of a breadcrumb, chevrons between them. Where it is short of
 * room the steps in the middle give way first, down to a few letters, and
 * the page's own name last, keeping enough to be read. here says the last
 * step is the page itself; a page's head on a phone gives only the steps
 * above it, since its title follows.
 */
export function CrumbTrail({ items, here = true, className = "" }: { items: Crumb[]; here?: boolean; className?: string }) {
  const t = useT();
  return (
    <nav aria-label={t("shell.breadcrumb")} className={`min-w-0 ${className}`}>
      <ol className="flex min-w-0 items-center gap-2 text-dense text-ink-3">
        {items.map((c, i) => {
          const current = here && i === items.length - 1;
          // The first step (the section) keeps its word; the page's own name shrinks least.
          const fit = current ? "min-w-20 shrink" : i === 0 ? "flex-none" : "min-w-12 shrink-[4]";
          return (
            <li key={i} className={`flex items-center gap-2 ${fit}`}>
              {i > 0 && <ChevronIcon width={14} height={14} className="flex-none" />}
              {current || !c.href ? (
                <span
                  aria-current={current ? "page" : undefined}
                  className={`truncate ${current ? "font-[550] text-ink" : "max-w-48"} ${c.mono ? "font-mono text-xs" : ""}`}
                  title={c.label}
                >
                  {c.label}
                </span>
              ) : (
                <Link
                  href={c.href}
                  title={c.label}
                  className={`block max-w-48 truncate hover:text-ink ${c.mono ? "font-mono text-xs text-ink-2" : ""}`}
                >
                  {c.label}
                </Link>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
