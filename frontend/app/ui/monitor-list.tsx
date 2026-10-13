"use client";

import { ConfirmDialog } from "@/app/ui/confirm";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useMemo, useState, useTransition } from "react";
import { type BulkAction, bulkMonitorsAction } from "@/app/actions";
import type { MonitorType } from "@/lib/api";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { monitorTypeOrder, typeLabel } from "@/lib/monitor-types";
import { compareNamespaces, MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import type { ListItem } from "@/lib/workspace";
import { ChevronIcon, PlusIcon, SearchIcon } from "@/app/ui/icons";
import { type DisplayState, stateStyles } from "@/app/ui/status";

type StateFilter = "all" | "problem" | "up" | "idle";

const problemStates: DisplayState[] = ["down", "recovering", "verifying"];
const idleStates: DisplayState[] = ["paused", "pending", "maintenance"];

function matchesState(item: ListItem, filter: StateFilter): boolean {
  switch (filter) {
    case "problem":
      return problemStates.includes(item.state);
    case "up":
      return item.state === "up";
    case "idle":
      return idleStates.includes(item.state);
    default:
      return true;
  }
}

/** Problems first, then by name. */
function byUrgency(a: ListItem, b: ListItem, locale: string): number {
  const rank = (i: ListItem) => (problemStates.includes(i.state) ? 0 : 1);
  return rank(a) - rank(b) || a.name.localeCompare(b.name, locale);
}

/** What the pill of a row says: the uptime, or why there is none. */
function pillText(item: ListItem, t: T): string {
  if (item.state === "paused") return t("monitorsList.list.pill.paused");
  if (item.uptime === null) return t("common.none");
  // Floor so that 99.96% is not shown as a perfect 100%.
  const floored = Math.floor(item.uptime * 10) / 10;
  return t("format.percent", { value: floored.toLocaleString(t.intl, { maximumFractionDigits: 1 }) });
}

/**
 * Every monitor, grouped by namespace, with search and filters. The data
 * comes from the server on each refresh; searching and filtering happen
 * here, so they are instant.
 */
export function MonitorList({
  items,
  beats,
  openByDefault,
  canEdit,
}: {
  items: ListItem[];
  /** How many recent checks a row has room for. */
  beats: number;
  /** Whether namespaces start opened; problems open theirs either way. */
  openByDefault: boolean;
  /** Whether the viewer may create and change monitors. */
  canEdit: boolean;
}) {
  const t = useT();
  const pathname = usePathname();
  const activeId = Number(/^\/monitors\/(\d+)/.exec(pathname)?.[1] ?? 0);

  const [query, setQuery] = useState("");
  const [stateFilter, setStateFilter] = useState<StateFilter>("all");
  const [typeFilter, setTypeFilter] = useState<MonitorType | "">("");
  const [tagFilter, setTagFilter] = useState("");
  // Only what the viewer toggled; everything else follows the default.
  const [toggled, setToggled] = useState<Record<string, boolean>>({});
  // In selecting mode a row is ticked instead of opened.
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [bulkError, setBulkError] = useState<string | null>(null);
  const [working, startBulk] = useTransition();

  // A monitor that was deleted elsewhere is no longer there to act on.
  const chosen = useMemo(() => items.filter((i) => selected.has(i.id)).map((i) => i.id), [items, selected]);

  function toggle(ids: number[], on: boolean) {
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

  const deletable = useMemo(() => items.filter((i) => selected.has(i.id)), [items, selected]);
  // Those kept in step with an annotated object stay deleted until restored.
  const managedChosen = deletable.filter((i) => i.managed).length;
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  function bulk(action: BulkAction) {
    const ids = action === "delete" ? deletable.map((i) => i.id) : chosen;
    setBulkError(null);
    startBulk(async () => {
      const { failed } = await bulkMonitorsAction(ids, action);
      if (failed > 0) setBulkError(t.n("monitorsList.list.bulk.failed", failed));
      else stopSelecting();
    });
  }

  const counts = useMemo(
    () => ({
      all: items.length,
      problem: items.filter((i) => matchesState(i, "problem")).length,
      up: items.filter((i) => matchesState(i, "up")).length,
      idle: items.filter((i) => matchesState(i, "idle")).length,
    }),
    [items],
  );
  const typesInUse = useMemo(
    () => monitorTypeOrder.filter((t) => items.some((i) => i.type === t)),
    [items],
  );
  const tagsInUse = useMemo(() => [...new Set(items.flatMap((i) => i.tags))].sort(), [items]);
  // A tag that no monitor has any more cannot be filtered on.
  const tag = tagsInUse.includes(tagFilter) ? tagFilter : "";

  const needle = query.trim().toLocaleLowerCase(t.intl);
  const filtering = needle !== "" || stateFilter !== "all" || typeFilter !== "" || tag !== "";
  const groups = useMemo(() => {
    const shown = items.filter(
      (i) =>
        matchesState(i, stateFilter) &&
        (typeFilter === "" || i.type === typeFilter) &&
        (tag === "" || i.tags.includes(tag)) &&
        (needle === "" ||
          `${i.name} ${i.target} ${i.namespace} ${i.tags.join(" ")}`.toLocaleLowerCase(t.intl).includes(needle)),
    );
    const byNamespace = new Map<string, ListItem[]>();
    for (const item of shown) {
      byNamespace.set(item.namespace, [...(byNamespace.get(item.namespace) ?? []), item]);
    }
    return [...byNamespace.entries()]
      .sort(([a], [b]) => compareNamespaces(a, b))
      .map(([namespace, group]) => ({ namespace, items: group.sort((a, b) => byUrgency(a, b, t.intl)) }));
  }, [items, needle, stateFilter, typeFilter, tag, t]);

  const filters: { key: StateFilter; label: string }[] = [
    { key: "all", label: t("common.all") },
    { key: "problem", label: t("monitorsList.state.problem") },
    { key: "up", label: t("monitorState.up") },
    { key: "idle", label: t("monitorsList.state.idle") },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-col gap-3 border-b border-line p-3">
        {canEdit && (
          <Link href="/monitors/new" className="btn btn-primary w-full">
            <PlusIcon /> {t("monitorsList.list.newMonitor")}
          </Link>
        )}
        <label className="relative block">
          <span className="sr-only">{t("monitorsList.list.search.label")}</span>
          <SearchIcon className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("monitorsList.list.search.placeholder")}
            className="field pl-9"
          />
        </label>
        <div className="flex flex-wrap items-center gap-1.5">
          {filters.map((f) => (
            <button
              key={f.key}
              type="button"
              onClick={() => setStateFilter(f.key)}
              aria-pressed={stateFilter === f.key}
              className={`rounded-full px-2.5 py-1 text-xs font-medium ${
                stateFilter === f.key
                  ? "bg-ink text-bg"
                  : f.key === "problem" && counts.problem > 0
                    ? "bg-down/12 text-down hover:bg-down/20"
                    : "bg-sunken text-muted hover:text-ink"
              }`}
            >
              {f.label} <span className="tabular-nums opacity-70">{counts[f.key]}</span>
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {typesInUse.length > 1 && (
            <select
              aria-label={t("monitorsList.list.typeFilter.label")}
              value={typeFilter}
              onChange={(e) => setTypeFilter(e.target.value as MonitorType | "")}
              className="rounded-full border-0 bg-sunken px-2 py-1 text-xs text-muted outline-none"
            >
              <option value="">{t("monitorsList.list.typeFilter.all")}</option>
              {typesInUse.map((type) => (
                <option key={type} value={type}>
                  {t(typeLabel(type))}
                </option>
              ))}
            </select>
          )}
          {tagsInUse.length > 0 && (
            <select
              aria-label={t("monitorsList.list.tagFilter.label")}
              value={tag}
              onChange={(e) => setTagFilter(e.target.value)}
              className="max-w-[9rem] rounded-full border-0 bg-sunken px-2 py-1 text-xs text-muted outline-none"
            >
              <option value="">{t("monitorsList.list.tagFilter.all")}</option>
              {tagsInUse.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          )}
          {canEdit && items.length > 0 && (
            <button
              type="button"
              onClick={() => (selecting ? stopSelecting() : setSelecting(true))}
              aria-pressed={selecting}
              className={`ml-auto rounded-full px-2.5 py-1 text-xs font-medium ${
                selecting ? "bg-ink text-bg" : "bg-sunken text-muted hover:text-ink"
              }`}
            >
              {selecting ? t("monitorsList.list.select.stop") : t("monitorsList.list.select.start")}
            </button>
          )}
        </div>
        {selecting && (
          <div className="flex flex-col gap-2 rounded-lg bg-sunken p-2.5" role="group" aria-label={t("monitorsList.list.bulk.label")}>
            <p className="text-xs text-muted">
              {chosen.length === 0 ? t("monitorsList.list.bulk.hint") : t.n("monitorsList.list.bulk.selected", chosen.length)}
            </p>
            <div className="flex flex-wrap gap-1.5">
              <button type="button" className="btn px-2.5 py-1 text-xs" disabled={chosen.length === 0 || working} onClick={() => bulk("pause")}>
                {t("monitorsList.list.bulk.pause")}
              </button>
              <button type="button" className="btn px-2.5 py-1 text-xs" disabled={chosen.length === 0 || working} onClick={() => bulk("resume")}>
                {t("monitorsList.list.bulk.resume")}
              </button>
              <button
                type="button"
                className="btn btn-danger px-2.5 py-1 text-xs"
                disabled={chosen.length === 0 || working}
                onClick={() => setConfirmingDelete(true)}
              >
                {t("common.delete")}
              </button>
              {confirmingDelete && (
                <ConfirmDialog
                  title={t("confirm.title")}
                  confirmLabel={t.n("monitorsList.list.bulk.deleteCount", deletable.length)}
                  onCancel={() => setConfirmingDelete(false)}
                  onConfirm={() => {
                    setConfirmingDelete(false);
                    bulk("delete");
                  }}
                >
                  <p>{t.n("monitorsList.list.bulk.confirmDelete", deletable.length)}</p>
                  <ul className="mt-2 max-h-40 overflow-auto rounded-lg bg-sunken px-3 py-2 text-ink">
                    {deletable.map((i) => (
                      <li key={i.id} className="truncate">
                        {i.name}
                      </li>
                    ))}
                  </ul>
                  {managedChosen > 0 && <p className="mt-3">{t.n("monitorsList.list.bulk.managedNote", managedChosen)}</p>}
                </ConfirmDialog>
              )}
            </div>
            {bulkError && (
              <p role="alert" className="text-xs text-down">
                {bulkError}
              </p>
            )}
          </div>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-2">
        {items.length === 0 && (
          <p className="p-4 text-center text-sm text-muted">
            {canEdit ? (
              <>
                {t("monitorsList.list.empty.before")}{" "}
                <Link href="/discovery" className="text-accent underline">
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
          <p className="p-4 text-center text-sm text-muted">{t("monitorsList.list.empty.filtered")}</p>
        )}
        {groups.map(({ namespace, items: group }) => {
          const problems = group.filter((i) => problemStates.includes(i.state)).length;
          // A search opens what it found; otherwise the viewer's choice, then the default.
          const open =
            filtering || (toggled[namespace] ?? (openByDefault || problems > 0 || group.some((i) => i.id === activeId)));
          return (
            <section key={namespace} aria-label={namespace || t(MANUAL_LABEL)} className="mb-1">
              <div className="flex items-center gap-1 rounded-lg px-1 py-1 text-xs text-muted">
                <button
                  type="button"
                  onClick={() => setToggled((t) => ({ ...t, [namespace]: !open }))}
                  aria-expanded={open}
                  className="flex min-w-0 flex-1 items-center gap-1 rounded-md px-1 py-0.5 text-left font-semibold uppercase tracking-wide hover:text-ink"
                >
                  <ChevronIcon
                    width={12}
                    height={12}
                    className={`shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
                  />
                  <span className="truncate">{namespace || t(MANUAL_LABEL)}</span>
                </button>
                {selecting && (
                  <input
                    type="checkbox"
                    aria-label={t("monitorsList.list.group.selectAll", { group: namespace || t(MANUAL_LABEL) })}
                    checked={group.every((i) => selected.has(i.id))}
                    onChange={(e) => toggle(group.map((i) => i.id), e.target.checked)}
                    className="h-3.5 w-3.5 accent-(--accent)"
                  />
                )}
                {problems > 0 && (
                  <span className="rounded-full bg-down/12 px-1.5 py-0.5 font-semibold tabular-nums text-down">
                    {problems}
                  </span>
                )}
                <Link
                  href={namespaceHref(namespace)}
                  title={t("monitorsList.list.group.page")}
                  className="rounded-md px-1.5 py-0.5 tabular-nums hover:bg-sunken hover:text-ink"
                >
                  {group.length}
                </Link>
              </div>
              {open && (
                <ul>
                  {group.map((item) => (
                    <Row
                      key={item.id}
                      item={item}
                      beats={beats}
                      active={item.id === activeId}
                      picked={selecting ? selected.has(item.id) : null}
                      onPick={(on) => toggle([item.id], on)}
                    />
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function Row({
  item,
  beats,
  active,
  picked,
  onPick,
}: {
  item: ListItem;
  beats: number;
  active: boolean;
  /** Whether the row is ticked; null when rows are not being selected. */
  picked: boolean | null;
  onPick: (on: boolean) => void;
}) {
  const t = useT();
  const style = stateStyles[item.state];
  const content = (
    <>
      <span
        title={t(style.label)}
        className={`w-14 shrink-0 rounded-full py-0.5 text-center text-xs font-semibold tabular-nums ${style.soft}`}
      >
        {pillText(item, t)}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{item.name}</span>
        <span className="block truncate text-xs text-muted">
          {item.kind ?? t(typeLabel(item.type))}
          {item.tags.length > 0 && ` · ${item.tags.join(", ")}`}
          {item.target && ` · ${item.target}`}
        </span>
      </span>
      <Beats beats={item.beats} slots={beats} />
    </>
  );
  const frame = "flex items-center gap-2.5 rounded-lg px-2 py-2";

  if (picked !== null) {
    return (
      <li>
        <label className={`${frame} cursor-pointer ${picked ? "bg-accent/10" : "hover:bg-sunken/60"}`}>
          <input
            type="checkbox"
            checked={picked}
            onChange={(e) => onPick(e.target.checked)}
            aria-label={t("monitorsList.list.row.select", { name: item.name })}
            className="h-4 w-4 shrink-0 accent-(--accent)"
          />
          {content}
        </label>
      </li>
    );
  }
  return (
    <li>
      <Link
        href={`/monitors/${item.id}`}
        aria-current={active ? "page" : undefined}
        className={`${frame} ${active ? "bg-sunken" : "hover:bg-sunken/60"}`}
      >
        {content}
      </Link>
    </li>
  );
}

/** The recent checks as small bars, oldest on the left. */
function Beats({ beats, slots }: { beats: string; slots: number }) {
  const t = useT();
  const shown = beats.slice(-slots);
  const failed = shown.split("0").length - 1;
  return (
    <span
      role="img"
      aria-label={t("monitorsList.list.beats.label", { count: shown.length, failed })}
      className="hidden h-4 shrink-0 items-stretch gap-px sm:flex"
    >
      {Array.from({ length: slots - shown.length }, (_, i) => (
        <span key={`e${i}`} className="w-1 rounded-full bg-line" />
      ))}
      {[...shown].map((beat, i) => (
        <span key={i} className={`w-1 rounded-full ${beat === "1" ? "bg-up" : "bg-down"}`} />
      ))}
    </span>
  );
}
