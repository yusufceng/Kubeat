"use client";

import { useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { matchScore } from "@/lib/palette-match";
import { urgencyRank } from "@/lib/urgency";
import { useMounted } from "@/lib/use-mounted";
import { NamespaceIcon } from "@/app/ui/fleet-icons";
import { AccountIcon, PlusIcon, SearchIcon } from "@/app/ui/icons";
import { adminSections, sections } from "@/app/ui/nav";
import { type DisplayState, stateStyles } from "@/app/ui/status";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** A monitor as the palette offers it (app/api/palette): enough to find it and show its state. */
export type PaletteMonitor = {
  id: number;
  name: string;
  /** The other names it is found by: that of its alerts, its object's (see otherNames). */
  aka: string;
  kind: string;
  namespace: string;
  state: DisplayState;
  target: string;
};

/** How many monitors the palette draws at once; typing narrows the rest down. */
const SHOWN_MONITORS = 50;

type Group = "actions" | "pages" | "namespaces" | "monitors";

type Entry = {
  key: string;
  group: Group;
  label: string;
  /** Set in mono: a namespace, a kind. */
  mono?: boolean;
  /** On the right, quiet: a namespace, a count. */
  hint?: string;
  href: string;
  icon: React.ReactNode;
  /** What a query is matched against; the first is the name. */
  fields: string[];
  /** For monitors: problems before the rest when nothing is typed. */
  rank?: number;
};

const groupOrder: Group[] = ["actions", "pages", "namespaces", "monitors"];

function entries(monitors: PaletteMonitor[], canEdit: boolean, admin: boolean, t: T): Entry[] {
  const out: Entry[] = [];
  if (canEdit) {
    const label = t("monitorsList.list.newMonitor");
    out.push({ key: "new", group: "actions", label, href: "/monitors/new", icon: <PlusIcon />, fields: [label] });
  }
  for (const s of [...sections, ...(admin ? adminSections : [])]) {
    const label = t(s.label);
    out.push({ key: s.href, group: "pages", label, href: s.href, icon: <s.icon />, fields: [label] });
  }
  const account = t("shell.account");
  out.push({ key: "/account", group: "pages", label: account, href: "/account", icon: <AccountIcon />, fields: [account] });

  const namespaces = new Map<string, number>();
  for (const m of monitors) namespaces.set(m.namespace, (namespaces.get(m.namespace) ?? 0) + 1);
  for (const [namespace, count] of [...namespaces].sort(([a], [b]) => (a === "" ? 1 : b === "" ? -1 : a.localeCompare(b)))) {
    const label = namespace || t(MANUAL_LABEL);
    out.push({
      key: `ns:${namespace}`,
      group: "namespaces",
      label,
      mono: namespace !== "",
      hint: t.n("monitorsList.overview.status.monitors", count),
      href: namespaceHref(namespace),
      icon: <NamespaceIcon />,
      fields: [label],
    });
  }
  for (const m of monitors) {
    const style = stateStyles[m.state];
    out.push({
      key: `m:${m.id}`,
      group: "monitors",
      label: m.name,
      hint: m.namespace || t(MANUAL_LABEL),
      href: `/monitors/${m.id}`,
      icon: <StatusGlyph kind={style.glyph} live={m.state === "down"} label={t(style.label)} />,
      fields: [m.name, m.namespace, m.kind, m.target, m.aka],
      rank: urgencyRank(m.state),
    });
  }
  return out;
}

/**
 * The command palette: ⌘K (Ctrl+K elsewhere) or the search button in the
 * context bar opens it anywhere in the app. One field finds a monitor (by
 * name, namespace, kind or address), a namespace or a page, and offers
 * "New monitor" to editors; the arrows choose, Enter goes, Escape closes.
 * It is a modal dialog: focus stays in it, and returns where it was.
 */
export function CommandPalette({ canEdit, admin }: { canEdit: boolean; admin: boolean }) {
  const t = useT();
  const mounted = useMounted();
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((now) => !now);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const mac = mounted && /mac|iphone|ipad/i.test(navigator.platform);
  return (
    <>
      <button
        type="button"
        className="fl-palette-trigger"
        onClick={() => setOpen(true)}
        aria-haspopup="dialog"
        aria-keyshortcuts="Meta+K Control+K"
        aria-label={t("palette.open")}
        title={t("palette.open")}
      >
        <SearchIcon />
        <span className="max-xl:hidden" aria-hidden>
          {t("palette.trigger")}
        </span>
        <kbd className="kbd max-lg:hidden" aria-hidden style={mounted ? undefined : { visibility: "hidden" }}>
          {mac ? "⌘K" : "Ctrl K"}
        </kbd>
      </button>
      {open && <PaletteDialog canEdit={canEdit} admin={admin} onClose={() => setOpen(false)} />}
    </>
  );
}

function PaletteDialog({ canEdit, admin, onClose }: { canEdit: boolean; admin: boolean; onClose: () => void }) {
  const t = useT();
  const router = useRouter();
  const dialog = useRef<HTMLDialogElement>(null);
  const id = useId();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const [monitors, setMonitors] = useState<PaletteMonitor[] | null>(null);
  const [failed, setFailed] = useState(false);

  // A modal dialog: the page behind is inert, Escape cancels, and closing
  // gives the focus back to where it was.
  useEffect(() => {
    const el = dialog.current;
    if (el && !el.open) el.showModal();
    return () => el?.close();
  }, []);

  /**
   * Closes while the dialog is still in the page, which is when the browser
   * gives the focus back to the button or field it came from; the cleanup
   * above runs once it is gone.
   */
  function close() {
    dialog.current?.close();
    onClose();
  }

  // The monitors are read when the palette opens, so that they are current.
  useEffect(() => {
    let live = true;
    fetch("/api/palette", { cache: "no-store" })
      .then((r) => (r.ok ? (r.json() as Promise<PaletteMonitor[]>) : Promise.reject(new Error(String(r.status)))))
      .then((m) => live && setMonitors(m))
      .catch(() => live && setFailed(true));
    return () => {
      live = false;
    };
  }, []);

  const all = useMemo(() => entries(monitors ?? [], canEdit, admin, t), [monitors, canEdit, admin, t]);

  // Each group ordered by how well it matches; the groups by their best
  // match, so that Enter goes to the best of all. With nothing typed, the
  // order is the menu's, with problems first among the monitors.
  const groups = useMemo(() => {
    const scored = all
      .map((e) => ({ e, score: matchScore(query, e.fields) }))
      .filter((s) => s.score > 0)
      .sort((a, b) => b.score - a.score || (a.e.rank ?? 0) - (b.e.rank ?? 0));
    return groupOrder
      .map((group, order) => {
        const matched = scored.filter((s) => s.e.group === group);
        const limit = group === "monitors" ? SHOWN_MONITORS : Infinity;
        return {
          group,
          order,
          best: matched[0]?.score ?? 0,
          entries: matched.slice(0, limit).map((s) => s.e),
          more: Math.max(0, matched.length - limit),
        };
      })
      .filter((g) => g.entries.length > 0)
      .sort((a, b) => (query.trim() ? b.best - a.best : 0) || a.order - b.order);
  }, [all, query]);
  const flat = groups.flatMap((g) => g.entries);
  const current = Math.min(active, flat.length - 1);
  const optionId = (i: number) => `${id}-o${i}`;

  // The chosen option stays in view as the arrows move it.
  useEffect(() => {
    if (current >= 0) document.getElementById(`${id}-o${current}`)?.scrollIntoView({ block: "nearest" });
  }, [id, current]);

  function go(entry: Entry | undefined) {
    if (!entry) return;
    close();
    router.push(entry.href);
  }

  /** The arrows (and Tab, which never leaves the field) go round; the page keys stop at the ends. */
  function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    const move = (by: number) => {
      e.preventDefault();
      if (flat.length > 0) setActive((current + by + flat.length) % flat.length);
    };
    const page = (by: number) => {
      e.preventDefault();
      setActive(Math.max(0, Math.min(flat.length - 1, current + by)));
    };
    if (e.key === "ArrowDown" || (e.key === "Tab" && !e.shiftKey)) move(1);
    else if (e.key === "ArrowUp" || (e.key === "Tab" && e.shiftKey)) move(-1);
    else if (e.key === "PageDown") page(8);
    else if (e.key === "PageUp") page(-8);
    else if (e.key === "Enter") {
      e.preventDefault();
      go(flat[current]);
    }
  }

  const groupTitle: Record<Group, string> = {
    actions: t("palette.group.actions"),
    pages: t("palette.group.pages"),
    namespaces: t("palette.group.namespaces"),
    monitors: t("palette.group.monitors"),
  };
  let index = -1;

  return (
    <dialog
      ref={dialog}
      className="fl-palette"
      aria-label={t("palette.label")}
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
      // Closed by the browser on its own (a second Escape, say). A close
      // event can also arrive late from an earlier close() while it is open
      // again; that one is not this dialog closing.
      onClose={() => !dialog.current?.open && onClose()}
      // A click on the backdrop is a click on the dialog itself.
      onClick={(e) => e.target === e.currentTarget && close()}
    >
      <div className="fl-palette-search">
        <SearchIcon className="flex-none text-ink-3" />
        <input
          autoFocus
          role="combobox"
          aria-expanded
          aria-controls={`${id}-list`}
          aria-activedescendant={current >= 0 ? optionId(current) : undefined}
          aria-autocomplete="list"
          aria-label={t("palette.placeholder")}
          placeholder={t("palette.placeholder")}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          spellCheck={false}
          autoComplete="off"
        />
        <kbd className="kbd" aria-hidden>
          esc
        </kbd>
      </div>
      <div id={`${id}-list`} role="listbox" aria-label={t("palette.label")} className="fl-palette-list">
        {groups.map((g) => (
          <div key={g.group} role="group" aria-labelledby={`${id}-${g.group}`}>
            <p id={`${id}-${g.group}`} role="presentation" className="fl-palette-group">
              {groupTitle[g.group]}
            </p>
            {g.entries.map((entry) => {
              index++;
              const i = index;
              return (
                <div
                  key={entry.key}
                  id={optionId(i)}
                  role="option"
                  aria-selected={i === current}
                  className="fl-palette-option"
                  onMouseMove={() => i !== current && setActive(i)}
                  onClick={() => go(entry)}
                >
                  <span className="fl-palette-icon">{entry.icon}</span>
                  <span className={`fl-palette-label ${entry.mono ? "type-mono" : ""}`}>{entry.label}</span>
                  {entry.hint && <span className="fl-palette-hint">{entry.hint}</span>}
                </div>
              );
            })}
            {g.more > 0 && <p className="fl-palette-note">{t("palette.more", { count: g.more })}</p>}
          </div>
        ))}
        {flat.length === 0 && monitors !== null && <p className="fl-palette-note">{t("palette.empty")}</p>}
        {monitors === null && !failed && <p className="fl-palette-note">{t("palette.loading")}</p>}
        {failed && <p className="fl-palette-note text-down-ink">{t("palette.failed")}</p>}
      </div>
      <p className="sr-only" aria-live="polite">
        {monitors !== null && t.n("palette.results", flat.length)}
      </p>
      <footer className="fl-palette-foot" aria-hidden>
        <span>
          <kbd className="kbd">↑</kbd>
          <kbd className="kbd">↓</kbd>
          {t("palette.keys.move")}
        </span>
        <span>
          <kbd className="kbd">↵</kbd>
          {t("palette.keys.open")}
        </span>
        <span>
          <kbd className="kbd">esc</kbd>
          {t("palette.keys.close")}
        </span>
      </footer>
    </dialog>
  );
}
