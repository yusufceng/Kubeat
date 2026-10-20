"use client";

import { usePathname, useRouter } from "next/navigation";
import { useT } from "@/lib/i18n/client";
import { inWorkspace } from "@/app/ui/nav";
import { type ListFilter, useListFilter } from "@/app/ui/shell-state";
import { type GlyphKind, StatusGlyph } from "@/app/ui/status-glyph";

/** Narrower than this, the monitor list is a part of the overview rather than a column. */
const NARROW = "(width < 64rem)";

/**
 * The fleet at a glance: how many monitors are up, down and degraded. Each
 * part filters the monitor list to those monitors, and a second click shows
 * them all again; where the list is not on screen, the part leads to it.
 */
export function PulsePill({ up, down, degraded }: { up: number; down: number; degraded: number }) {
  const t = useT();
  const router = useRouter();
  const pathname = usePathname();
  const [filter, setFilter] = useListFilter();

  function show(part: ListFilter) {
    const narrow = matchMedia(NARROW).matches;
    // On a wide screen the workspace has the list beside it; on a narrow
    // one only the overview has it, below the overview itself.
    const listHere = narrow ? pathname === "/" : inWorkspace(pathname);
    setFilter(listHere && filter === part ? "all" : part);
    if (!listHere) router.push(narrow ? "/#monitors" : "/");
    else if (narrow) document.getElementById("monitors")?.scrollIntoView({ block: "start" });
  }

  const part = (key: ListFilter, kind: GlyphKind, count: number, word: string, tone = "") => (
    <button
      type="button"
      onClick={() => show(key)}
      aria-pressed={filter === key}
      title={t("shell.pulse.show", { what: `${count} ${word}` })}
      className={`flex h-6 items-center gap-1.5 rounded-full px-2 hover:bg-hover aria-pressed:bg-hover ${tone}`}
    >
      <StatusGlyph kind={kind} live={kind === "down"} />
      <span>{count}</span>
      {/* Below 1280px the breadcrumb needs the room: the glyphs say enough. */}
      <span className="max-xl:sr-only">{word}</span>
    </button>
  );

  return (
    <div
      role="group"
      aria-label={t("shell.pulse.label")}
      className="flex h-[30px] items-center gap-0.5 rounded-full border border-line bg-panel px-[3px] text-xs font-[550] tabular-nums"
    >
      {part("up", "up", up, t("shell.pulse.up"))}
      {down > 0 && part("down", "down", down, t("shell.pulse.down"), "text-down-ink")}
      {degraded > 0 && part("degraded", "warn", degraded, t("shell.pulse.degraded"), "text-warn-ink")}
    </div>
  );
}
