"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useCallback, useEffect, useId, useMemo, useRef, useState, useTransition } from "react";
import { type BulkAction, bulkMonitorsAction } from "@/app/actions";
import type { MonitorType } from "@/lib/api";
import type { FleetItem } from "@/lib/fleet";
import { useT } from "@/lib/i18n/client";
import { monitorTypeOrder, typeLabel } from "@/lib/monitor-types";
import { compareNamespaces, MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { byUrgency } from "@/lib/urgency";
import { ConfirmDialog } from "@/app/ui/confirm";
import { CheckIcon, CloseIcon, FilterIcon } from "@/app/ui/fleet-icons";
import { SearchIcon } from "@/app/ui/icons";
import type { UptimeTones } from "@/lib/uptime-tone";
import { Chevron, groupCount, HealthBar, HoverCard, Row } from "@/app/ui/monitor-row";
import { type ListFilter, useListFilter } from "@/app/ui/shell-state";
import type { DisplayState } from "@/app/ui/status";

const degradedStates: DisplayState[] = ["verifying", "recovering", "degraded"];
const idleStates: DisplayState[] = ["paused", "pending", "maintenance"];

/** How long the pointer rests on a row before its card shows (the design's 400ms). */
const CARD_DELAY_MS = 400;

function matchesState(item: FleetItem, filter: ListFilter): boolean {
  switch (filter) {
    case "up":
      return item.state === "up";
    case "down":
      return item.state === "down";
    case "degraded":
      return degradedStates.includes(item.state);
    case "idle":
      return idleStates.includes(item.state);
    default:
      return true;
  }
}

/**
 * Every monitor, grouped by namespace, with a filter, a segmented state
 * control and, in a menu, the type and tag filters. The data comes from
 * the server on each refresh; filtering happens here, so it is instant.
 *
 * Editors select monitors with the checkbox that takes the glyph's place
 * on hover (or with x on a focused row, or "Select monitors" in the menu),
 * and act on them from the bar at the foot of the list. The keyboard moves
 * between rows with the arrows; / jumps to the filter.
 */
export function MonitorList({
  items,
  beats,
  openByDefault,
  canEdit,
  tones,
}: {
  items: FleetItem[];
  /** How many recent checks a row has room for. */
  beats: number;
  /** Whether namespaces start opened; problems open theirs either way. */
  openByDefault: boolean;
  /** Whether the viewer may create and change monitors. */
  canEdit: boolean;
  tones: UptimeTones;
}) {
  const t = useT();
  const pathname = usePathname();
  const activeId = Number(/^\/monitors\/(\d+)/.exec(pathname)?.[1] ?? 0);
  const search = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const hintId = useId();

  const [query, setQuery] = useState("");
  // Shared with the fleet pulse of the context bar, which can set it too.
  const [stateFilter, setStateFilter] = useListFilter();
  const [typeFilter, setTypeFilter] = useState<MonitorType | "">("");
  const [tagFilter, setTagFilter] = useState("");
  // Only what the viewer toggled; everything else follows the default.
  const [toggled, setToggled] = useState<Record<string, boolean>>({});

  // Selecting: the checkboxes show on every row while anything is ticked,
  // or once the viewer asked to select.
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [bulkError, setBulkError] = useState<string | null>(null);
  const [working, startBulk] = useTransition();
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const selectMode = canEdit && (selecting || selected.size > 0);

  // A monitor that was deleted elsewhere is no longer there to act on.
  const chosen = useMemo(() => items.filter((i) => selected.has(i.id)), [items, selected]);
  // Those kept in step with an annotated object stay deleted until restored.
  const managedChosen = chosen.filter((i) => i.managed).length;

  const pick = useCallback((id: number, on: boolean) => {
    setSelected((current) => {
      const next = new Set(current);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);

  function pickAll(ids: number[], on: boolean) {
    setSelected((current) => {
      const next = new Set(current);
      for (const id of ids) {
        if (on) next.add(id);
        else next.delete(id);
      }
      return next;
    });
  }

  function stopSelecting() {
    setSelecting(false);
    setSelected(new Set());
    setBulkError(null);
  }

  function bulk(action: BulkAction) {
    const ids = chosen.map((i) => i.id);
    setBulkError(null);
    startBulk(async () => {
      const { failed } = await bulkMonitorsAction(ids, action);
      if (failed > 0) setBulkError(t.n("monitorsList.list.bulk.failed", failed));
      else stopSelecting();
    });
  }

  // The card of the row the pointer rests on. Once one shows, moving to
  // the next row shows that one at once; leaving the list hides it.
  const [hover, setHover] = useState<{ id: number; row: HTMLElement } | null>(null);
  const hoverTimer = useRef<number | undefined>(undefined);
  const cardShown = useRef(false);
  const onHover = useCallback((id: number | null, row: HTMLElement | null) => {
    window.clearTimeout(hoverTimer.current);
    if (id === null || row === null) {
      hoverTimer.current = window.setTimeout(() => {
        cardShown.current = false;
        setHover(null);
      }, 120);
      return;
    }
    if (cardShown.current) {
      setHover({ id, row });
      return;
    }
    hoverTimer.current = window.setTimeout(() => {
      cardShown.current = true;
      setHover({ id, row });
    }, CARD_DELAY_MS);
  }, []);
  useEffect(() => () => window.clearTimeout(hoverTimer.current), []);
  const hovered = hover ? items.find((i) => i.id === hover.id) : undefined;
  // Scrolling moves the rows from under the card: it goes, and the next one waits again.
  function hideCard() {
    window.clearTimeout(hoverTimer.current);
    cardShown.current = false;
    if (hover) setHover(null);
  }

  // "/" puts the cursor in the filter, from anywhere but a field.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
      const target = e.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable]")) return;
      const field = search.current;
      if (!field || field.offsetParent === null) return; // the list is not on screen
      e.preventDefault();
      field.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // The monitor that was opened (from the palette, say) comes into view.
  useEffect(() => {
    if (!activeId) return;
    list.current?.querySelector(`[data-row="${activeId}"]`)?.scrollIntoView({ block: "nearest" });
  }, [activeId]);

  const counts = useMemo(
    () => ({
      all: items.length,
      up: items.filter((i) => matchesState(i, "up")).length,
      down: items.filter((i) => matchesState(i, "down")).length,
      degraded: items.filter((i) => matchesState(i, "degraded")).length,
      idle: items.filter((i) => matchesState(i, "idle")).length,
    }),
    [items],
  );
  const typesInUse = useMemo(() => monitorTypeOrder.filter((type) => items.some((i) => i.type === type)), [items]);
  const tagsInUse = useMemo(() => [...new Set(items.flatMap((i) => i.tags))].sort(), [items]);
  // A filter on something no monitor has any more filters nothing.
  const type = typesInUse.includes(typeFilter as MonitorType) ? typeFilter : "";
  const tag = tagsInUse.includes(tagFilter) ? tagFilter : "";

  const needle = query.trim().toLocaleLowerCase(t.intl);
  const filtering = needle !== "" || stateFilter !== "all" || type !== "" || tag !== "";
  const groups = useMemo(() => {
    const shown = items.filter(
      (i) =>
        matchesState(i, stateFilter) &&
        (type === "" || i.type === type) &&
        (tag === "" || i.tags.includes(tag)) &&
        (needle === "" ||
          `${i.name} ${i.aka} ${i.kindShort} ${i.target} ${i.namespace} ${i.tags.join(" ")}`.toLocaleLowerCase(t.intl).includes(needle)),
    );
    const byNamespace = new Map<string, FleetItem[]>();
    for (const item of shown) byNamespace.set(item.namespace, [...(byNamespace.get(item.namespace) ?? []), item]);
    const order = byUrgency<FleetItem>(t.intl);
    return [...byNamespace.entries()]
      .sort(([a], [b]) => compareNamespaces(a, b))
      .map(([namespace, group]) => ({ namespace, items: group.sort(order) }));
  }, [items, needle, stateFilter, type, tag, t]);

  /** Arrows move between rows, Home and End to the ends, x ticks the row, Escape stops selecting. */
  function onKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    if (e.key === "Escape" && selectMode) {
      stopSelecting();
      return;
    }
    const row = (e.target as HTMLElement).closest<HTMLElement>("a[data-row]");
    if (!row) return;
    const rows = [...(list.current?.querySelectorAll<HTMLElement>("a[data-row]") ?? [])];
    const at = rows.indexOf(row);
    const go = (i: number) => {
      e.preventDefault();
      rows[Math.max(0, Math.min(rows.length - 1, i))]?.focus();
    };
    if (e.key === "ArrowDown") go(at + 1);
    else if (e.key === "ArrowUp") go(at - 1);
    else if (e.key === "Home") go(0);
    else if (e.key === "End") go(rows.length - 1);
    else if (e.key.toLowerCase() === "x" && canEdit) {
      e.preventDefault();
      const id = Number(row.dataset.row);
      pick(id, !selected.has(id));
    }
  }

  const filters: { key: ListFilter; label: string }[] = [
    { key: "all", label: t("monitorsList.filter.all") },
    { key: "up", label: t("monitorsList.filter.up") },
    { key: "down", label: t("monitorsList.filter.down") },
    { key: "degraded", label: t("monitorsList.filter.degraded") },
    { key: "idle", label: t("monitorsList.filter.idle") },
  ];

  return (
    <div className="fl-list" data-list data-selecting={selectMode || undefined}>
      {/* With nothing to filter there is no toolbar, only the way to begin. */}
      <div className="fl-toolbar" hidden={items.length === 0}>
        <div className="flex items-center gap-2">
          <label className="fl-search">
            <span className="sr-only">{t("monitorsList.list.search.label")}</span>
            <SearchIcon className="pointer-events-none flex-none text-ink-3" />
            <input
              ref={search}
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("monitorsList.list.search.placeholder")}
              aria-keyshortcuts="/"
            />
            <kbd className="kbd fl-search-kbd" aria-hidden>
              /
            </kbd>
          </label>
          {(typesInUse.length > 1 || tagsInUse.length > 0 || (canEdit && items.length > 0)) && (
            <FilterMenu
              types={typesInUse.length > 1 ? typesInUse : []}
              tags={tagsInUse}
              type={type}
              tag={tag}
              onType={setTypeFilter}
              onTag={setTagFilter}
              onSelect={canEdit && items.length > 0 ? () => setSelecting(true) : undefined}
            />
          )}
        </div>
        {/* A segmented control; narrow widths and long Turkish names may wrap it. */}
        <div role="group" aria-label={t("monitorsList.filter.label")} className="fl-seg">
          {filters.map((f) => (
            <button key={f.key} type="button" onClick={() => setStateFilter(f.key)} aria-pressed={stateFilter === f.key}>
              {f.label}
              <span
                className={`font-semibold tabular-nums ${
                  f.key === "down" && counts.down > 0
                    ? "text-down-ink"
                    : f.key === "degraded" && counts.degraded > 0
                      ? "text-warn-ink"
                      : "text-ink-3"
                }`}
              >
                {counts[f.key]}
              </span>
            </button>
          ))}
        </div>
        {(type || tag) && (
          <div className="flex flex-wrap gap-1.5" aria-label={t("monitorsList.list.filters.active")} role="group">
            {type && <FilterChip label={t(typeLabel(type))} onRemove={() => setTypeFilter("")} />}
            {tag && <FilterChip label={tag} onRemove={() => setTagFilter("")} />}
          </div>
        )}
      </div>

      <p id={hintId} className="sr-only">
        {canEdit ? t("monitorsList.list.keys.edit") : t("monitorsList.list.keys.view")}
      </p>
      <div
        ref={list}
        className="fl-groups"
        onKeyDown={onKeyDown}
        onScroll={hideCard}
      >
        {items.length === 0 && (
          <p className="px-4 py-6 text-center text-sm text-ink-3">
            {canEdit ? (
              <>
                {t("monitorsList.list.empty.before")}{" "}
                <Link href="/discovery" className="text-accent-text underline">
                  {t("monitorsList.list.empty.link")}
                </Link>
                .
              </>
            ) : (
              t("monitorsList.list.empty.viewer")
            )}
          </p>
        )}
        {items.length > 0 && groups.length === 0 && (
          <p className="px-4 py-6 text-center text-sm text-ink-3">{t("monitorsList.list.empty.filtered")}</p>
        )}
        {groups.map(({ namespace, items: group }) => {
          const label = namespace || t(MANUAL_LABEL);
          const problems = group.some((i) => i.state === "down" || degradedStates.includes(i.state));
          // A search opens what it found; otherwise the viewer's choice, then the default.
          const open = filtering || (toggled[namespace] ?? (openByDefault || problems || group.some((i) => i.id === activeId)));
          const count = groupCount(group, t);
          const allPicked = group.every((i) => selected.has(i.id));
          return (
            <section
              key={namespace}
              aria-label={label}
              className="fl-group"
              style={{ containIntrinsicSize: `auto ${30 + (open ? group.length * 34 : 0)}px` }}
            >
              <div className="fl-ns">
                {selectMode && (
                  <span className="fl-lead">
                    <input
                      type="checkbox"
                      className="fl-pick"
                      checked={allPicked}
                      onChange={(e) => pickAll(group.map((i) => i.id), e.target.checked)}
                      aria-label={t("monitorsList.list.group.selectAll", { group: label })}
                    />
                  </span>
                )}
                <button
                  type="button"
                  onClick={() => setToggled((current) => ({ ...current, [namespace]: !open }))}
                  aria-expanded={open}
                  className="fl-ns-toggle"
                >
                  {!selectMode && <Chevron open={open} />}
                  <span className={namespace ? "fl-ns-name" : "fl-ns-name fl-ns-manual"}>{label}</span>
                </button>
                <HealthBar items={group} />
                <Link
                  href={namespaceHref(namespace)}
                  className={`fl-ns-count ${count.tone}`}
                  title={t("monitorsList.list.group.page")}
                  aria-label={`${count.text}, ${t("monitorsList.list.group.pageOf", { group: label })}`}
                >
                  {count.text}
                </Link>
              </div>
              {open && (
                <ul>
                  {group.map((item) => (
                    <Row
                      key={item.id}
                      item={item}
                      // The keys are told once, at the first row.
                      describedBy={item === groups[0].items[0] ? hintId : undefined}
                      beats={beats}
                      active={item.id === activeId}
                      picked={selected.has(item.id)}
                      canPick={canEdit}
                      selecting={selectMode}
                      onPick={pick}
                      tones={tones}
                      onHover={onHover}
                    />
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>

      {selectMode && (
        <div className="fl-bulk pop" role="group" aria-label={t("monitorsList.list.bulk.label")}>
          <p className="min-w-0 flex-1 truncate text-xs font-medium tabular-nums" aria-live="polite">
            {chosen.length === 0 ? t("monitorsList.list.bulk.hint") : t.n("monitorsList.list.bulk.selected", chosen.length)}
          </p>
          <button type="button" className="btn btn-ghost btn-sm" disabled={chosen.length === 0 || working} onClick={() => bulk("pause")}>
            {t("monitorsList.list.bulk.pause")}
          </button>
          <button type="button" className="btn btn-ghost btn-sm" disabled={chosen.length === 0 || working} onClick={() => bulk("resume")}>
            {t("monitorsList.list.bulk.resume")}
          </button>
          <button
            type="button"
            className="btn btn-ghost btn-sm fl-bulk-delete"
            disabled={chosen.length === 0 || working}
            onClick={() => setConfirmingDelete(true)}
          >
            {t("common.delete")}
          </button>
          <button
            type="button"
            className="btn btn-ghost btn-sm fl-icon-btn"
            onClick={stopSelecting}
            aria-label={t("monitorsList.list.bulk.clear")}
            title={t("monitorsList.list.bulk.clear")}
          >
            <CloseIcon />
          </button>
          {bulkError && (
            <p role="alert" className="basis-full text-xs text-down-ink">
              {bulkError}
            </p>
          )}
        </div>
      )}
      {confirmingDelete && (
        <ConfirmDialog
          title={t.n("monitorsList.list.bulk.deleteTitle", chosen.length)}
          confirmLabel={t.n("monitorsList.list.bulk.deleteCount", chosen.length)}
          onCancel={() => setConfirmingDelete(false)}
          onConfirm={() => {
            setConfirmingDelete(false);
            bulk("delete");
          }}
        >
          <p>{t.n("monitorsList.list.bulk.confirmDelete", chosen.length)}</p>
          <ul className="type-mono mt-3 max-h-40 overflow-auto rounded-sm border border-line bg-well px-3 py-2 text-ink">
            {chosen.map((i) => (
              <li key={i.id} className="truncate">
                {i.namespace ? `${i.namespace}/` : ""}
                {i.name}
              </li>
            ))}
          </ul>
          {managedChosen > 0 && <p className="mt-3">{t.n("monitorsList.list.bulk.managedNote", managedChosen)}</p>}
        </ConfirmDialog>
      )}
      {hover && hovered && <HoverCard item={hovered} anchor={hover.row} />}
    </div>
  );
}

/** A filter that is on, with the way to take it off. */
function FilterChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  const t = useT();
  return (
    <span className="fl-chip">
      <span className="truncate">{label}</span>
      <button type="button" onClick={onRemove} aria-label={t("monitorsList.list.filters.remove", { filter: label })}>
        <CloseIcon width={12} height={12} />
      </button>
    </span>
  );
}

/** The menu's width, fixed so that it can be placed before it shows. */
const MENU_WIDTH = 256;

/**
 * The filters that are not on view: type and tag, as selects in a menu
 * under its button; and for editors, the way into selecting monitors
 * where there is no pointer to hover with.
 */
function FilterMenu({
  types,
  tags,
  type,
  tag,
  onType,
  onTag,
  onSelect,
}: {
  types: MonitorType[];
  tags: string[];
  type: MonitorType | "";
  tag: string;
  onType: (type: MonitorType | "") => void;
  onTag: (tag: string) => void;
  onSelect?: () => void;
}) {
  const t = useT();
  const id = useId();
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const active = (type ? 1 : 0) + (tag ? 1 : 0);

  // Under the button, with its right edge on the button's, inside the
  // window; it closes when the page moves under it.
  useEffect(() => {
    const el = menu.current;
    if (!el) return;
    const place = (e: Event) => {
      if ((e as ToggleEvent).newState !== "open" || !button.current) return;
      const r = button.current.getBoundingClientRect();
      el.style.top = `${r.bottom + 6}px`;
      el.style.left = `${Math.max(8, Math.min(r.right - MENU_WIDTH, window.innerWidth - MENU_WIDTH - 8))}px`;
    };
    const close = (e: Event) => {
      if (e.type === "scroll" && el.contains(e.target as Node)) return;
      if (el.matches(":popover-open")) el.hidePopover();
    };
    el.addEventListener("beforetoggle", place);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      el.removeEventListener("beforetoggle", place);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, []);

  const label = active > 0 ? t("monitorsList.list.filters.labelActive", { count: active }) : t("monitorsList.list.filters.label");
  return (
    <>
      <button
        ref={button}
        type="button"
        popoverTarget={id}
        className="fl-filter-btn"
        aria-label={label}
        title={label}
        data-active={active > 0 || undefined}
      >
        <FilterIcon />
        {active > 0 && <span className="fl-filter-count">{active}</span>}
      </button>
      <div id={id} ref={menu} popover="auto" className="menu inset-auto" style={{ width: MENU_WIDTH }}>
        {types.length > 0 && (
          <label className="fl-menu-field">
            <span>{t("monitorsList.list.typeFilter.label")}</span>
            <select className="field" value={type} onChange={(e) => onType(e.target.value as MonitorType | "")}>
              <option value="">{t("monitorsList.list.typeFilter.all")}</option>
              {types.map((value) => (
                <option key={value} value={value}>
                  {t(typeLabel(value))}
                </option>
              ))}
            </select>
          </label>
        )}
        {tags.length > 0 && (
          <label className="fl-menu-field">
            <span>{t("monitorsList.list.tagFilter.label")}</span>
            <select className="field" value={tag} onChange={(e) => onTag(e.target.value)}>
              <option value="">{t("monitorsList.list.tagFilter.all")}</option>
              {tags.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
        )}
        {active > 0 && (
          <button
            type="button"
            className="menu-item"
            onClick={() => {
              onType("");
              onTag("");
            }}
          >
            <CloseIcon />
            {t("monitorsList.list.filters.clear")}
          </button>
        )}
        {onSelect && (
          <>
            {(types.length > 0 || tags.length > 0) && <hr className="menu-sep" />}
            <button
              type="button"
              className="menu-item"
              onClick={() => {
                onSelect();
                menu.current?.hidePopover();
              }}
            >
              <CheckIcon />
              {t("monitorsList.list.select.start")}
            </button>
          </>
        )}
      </div>
    </>
  );
}
