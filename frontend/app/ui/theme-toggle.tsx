"use client";

import { useSyncExternalStore } from "react";
import { DisplayIcon, MoonIcon, SunIcon } from "@/app/ui/icons";

/** localStorage key of the viewer's choice; absent means "follow the system". */
export const THEME_KEY = "kubeat-theme";

/**
 * Runs before the first paint, from the root layout: sets data-theme on
 * <html> from the stored choice or the system's preference, so that the
 * page never flashes in the wrong theme.
 */
export const themeScript = `(function(){try{var t=localStorage.getItem("${THEME_KEY}");if(t!=="light"&&t!=="dark"){t=matchMedia("(prefers-color-scheme: dark)").matches?"dark":"light"}document.documentElement.dataset.theme=t}catch(e){document.documentElement.dataset.theme="light"}})()`;

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
const labels: Record<Choice, string> = { system: "Sistem", light: "Açık", dark: "Koyu" };
const icons = { system: DisplayIcon, light: SunIcon, dark: MoonIcon };

/** Cycles the theme: follow the system, light, dark. */
export function ThemeToggle() {
  const choice = useSyncExternalStore<Choice>(subscribe, read, () => "system");
  const Icon = icons[choice];
  const next = order[(order.indexOf(choice) + 1) % order.length];

  return (
    <button
      type="button"
      onClick={() => choose(next)}
      title={`Tema: ${labels[choice]}. Değiştirmek için tıklayın (${labels[next]}).`}
      aria-label={`Tema: ${labels[choice]}`}
      className="inline-flex h-8 w-8 items-center justify-center rounded-lg text-muted hover:bg-sunken hover:text-ink"
    >
      <Icon />
    </button>
  );
}
