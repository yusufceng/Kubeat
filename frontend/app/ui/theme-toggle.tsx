"use client";

import { useEffect, useSyncExternalStore } from "react";
import { DisplayIcon, MoonIcon, SunIcon } from "@/app/ui/icons";
import { useT } from "@/lib/i18n/client";

/** localStorage key of the viewer's choice; absent means "follow the system". */
export const THEME_KEY = "kubeat-theme";

/**
 * Runs before the first paint, from the root layout: sets data-theme on
 * <html> from the stored choice or the system's preference, so that the
 * page never flashes in the wrong theme. Public status pages follow the
 * system alone: their visitors never chose a theme here.
 */
export const themeScript = `(function(){try{var t=localStorage.getItem("${THEME_KEY}");if(location.pathname.indexOf("/status/")===0||(t!=="light"&&t!=="dark")){t=matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light"}document.documentElement.dataset.theme=t}catch(e){document.documentElement.dataset.theme="light"}})()`;

type Choice = "system" | "light" | "dark";

const SYSTEM_DARK = "(prefers-color-scheme: dark)";

function read(): Choice {
  try {
    const stored = localStorage.getItem(THEME_KEY);
    return stored === "light" || stored === "dark" ? stored : "system";
  } catch {
    return "system"; // storage is blocked; the system decides
  }
}

function apply(choice: Choice) {
  const dark = choice === "dark" || (choice === "system" && matchMedia(SYSTEM_DARK).matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}

const listeners = new Set<() => void>();

function subscribe(onChange: () => void) {
  listeners.add(onChange);
  // The system's theme changing matters only while it is being followed.
  const system = matchMedia(SYSTEM_DARK);
  const onSystem = () => apply(read());
  system.addEventListener("change", onSystem);
  // Another tab changed the choice.
  const onStorage = (e: StorageEvent) => {
    if (e.key !== THEME_KEY) return;
    apply(read());
    onChange();
  };
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(onChange);
    system.removeEventListener("change", onSystem);
    window.removeEventListener("storage", onStorage);
  };
}

function choose(choice: Choice) {
  try {
    if (choice === "system") localStorage.removeItem(THEME_KEY);
    else localStorage.setItem(THEME_KEY, choice);
  } catch {
    // Without storage the choice lasts until the page is left.
  }
  apply(choice);
  listeners.forEach((notify) => notify());
}

const order: Choice[] = ["system", "light", "dark"];
/** The message key of each choice's name. */
const labels: Record<Choice, string> = { system: "theme.system", light: "theme.light", dark: "theme.dark" };
const icons = { system: DisplayIcon, light: SunIcon, dark: MoonIcon };

/**
 * Cycles the theme: follow the system, light, dark. In the rail it is an
 * icon with a tooltip; in a menu, a row that says the theme.
 */
export function ThemeToggle({ variant = "rail" }: { variant?: "rail" | "menu" }) {
  const choice = useSyncExternalStore<Choice>(subscribe, read, () => "system");
  const t = useT();
  const Icon = icons[choice];
  const next = order[(order.indexOf(choice) + 1) % order.length];
  const name = t("theme.label", { theme: t(labels[choice]) });

  if (variant === "menu") {
    return (
      <button type="button" onClick={() => choose(next)} className="menu-item" title={t("theme.switchHint", { theme: t(labels[choice]), next: t(labels[next]) })}>
        <Icon />
        {name}
      </button>
    );
  }
  return (
    <button
      type="button"
      onClick={() => choose(next)}
      aria-label={name}
      className="rail-item"
    >
      <Icon width={20} height={20} />
      <span className="rail-tip" aria-hidden>
        {t("theme.switchHint", { theme: t(labels[choice]), next: t(labels[next]) })}
      </span>
    </button>
  );
}

/**
 * Keeps a public status page in the system's theme, also when it was
 * reached from inside the app, and hands the app back its own choice when
 * the visitor leaves.
 */
export function SystemTheme() {
  useEffect(() => {
    const system = matchMedia(SYSTEM_DARK);
    const follow = () => apply("system");
    follow();
    system.addEventListener("change", follow);
    return () => {
      system.removeEventListener("change", follow);
      apply(read());
    };
  }, []);
  return null;
}
