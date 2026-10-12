"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useMemo, useState } from "react";
import type { MonitorType } from "@/lib/api";
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
function byUrgency(a: ListItem, b: ListItem): number {
  const rank = (i: ListItem) => (problemStates.includes(i.state) ? 0 : 1);
  return rank(a) - rank(b) || a.name.localeCompare(b.name, "tr");
}

/** What the pill of a row says: the uptime, or why there is none. */
function pillText(item: ListItem): string {
  if (item.state === "paused") return "Durdu";
  if (item.uptime === null) return "–";
  // Floor so that 99.96% is not shown as a perfect 100%.
  const floored = Math.floor(item.uptime * 10) / 10;
  return `%${floored.toLocaleString("tr-TR", { maximumFractionDigits: 1 })}`;
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
}: {
  items: ListItem[];
  /** How many recent checks a row has room for. */
  beats: number;
  /** Whether namespaces start opened; problems open theirs either way. */
  openByDefault: boolean;
}) {
  const pathname = usePathname();
  const activeId = Number(/^\/monitors\/(\d+)/.exec(pathname)?.[1] ?? 0);

  const [query, setQuery] = useState("");
  const [stateFilter, setStateFilter] = useState<StateFilter>("all");
  const [typeFilter, setTypeFilter] = useState<MonitorType | "">("");
  // Only what the viewer toggled; everything else follows the default.
  const [toggled, setToggled] = useState<Record<string, boolean>>({});

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

  const needle = query.trim().toLocaleLowerCase("tr");
  const filtering = needle !== "" || stateFilter !== "all" || typeFilter !== "";
  const groups = useMemo(() => {
    const shown = items.filter(
      (i) =>
        matchesState(i, stateFilter) &&
        (typeFilter === "" || i.type === typeFilter) &&
        (needle === "" ||
          `${i.name} ${i.target} ${i.namespace}`.toLocaleLowerCase("tr").includes(needle)),
    );
    const byNamespace = new Map<string, ListItem[]>();
    for (const item of shown) {
      byNamespace.set(item.namespace, [...(byNamespace.get(item.namespace) ?? []), item]);
    }
    return [...byNamespace.entries()]
      .sort(([a], [b]) => compareNamespaces(a, b))
      .map(([namespace, group]) => ({ namespace, items: group.sort(byUrgency) }));
  }, [items, needle, stateFilter, typeFilter]);

  const filters: { key: StateFilter; label: string }[] = [
    { key: "all", label: "Tümü" },
    { key: "problem", label: "Sorunlu" },
    { key: "up", label: "Çalışıyor" },
    { key: "idle", label: "Pasif" },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-col gap-3 border-b border-line p-3">
        <Link href="/monitors/new" className="btn btn-primary w-full">
          <PlusIcon /> Yeni monitor
        </Link>
        <label className="relative block">
          <span className="sr-only">Monitor ara</span>
          <SearchIcon className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Ad, adres ya da namespace ara"
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
          {typesInUse.length > 1 && (
            <select
              aria-label="Tipe göre süz"
              value={typeFilter}
              onChange={(e) => setTypeFilter(e.target.value as MonitorType | "")}
              className="ml-auto rounded-full border-0 bg-sunken px-2 py-1 text-xs text-muted outline-none"
            >
              <option value="">Tüm tipler</option>
              {typesInUse.map((t) => (
                <option key={t} value={t}>
                  {typeLabel(t)}
                </option>
              ))}
            </select>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-2">
        {items.length === 0 && (
          <p className="p-4 text-center text-sm text-muted">
            Henüz monitor yok. Yukarıdaki düğmeyle ekleyin ya da{" "}
            <Link href="/discovery" className="text-accent underline">
              cluster&apos;dan keşfedin
            </Link>
            .
          </p>
        )}
        {items.length > 0 && groups.length === 0 && (
          <p className="p-4 text-center text-sm text-muted">Bu süzgece uyan monitor yok.</p>
        )}
        {groups.map(({ namespace, items: group }) => {
          const problems = group.filter((i) => problemStates.includes(i.state)).length;
          // A search opens what it found; otherwise the viewer's choice, then the default.
          const open =
            filtering || (toggled[namespace] ?? (openByDefault || problems > 0 || group.some((i) => i.id === activeId)));
          return (
            <section key={namespace} aria-label={namespace || MANUAL_LABEL} className="mb-1">
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
                  <span className="truncate">{namespace || MANUAL_LABEL}</span>
                </button>
                {problems > 0 && (
                  <span className="rounded-full bg-down/12 px-1.5 py-0.5 font-semibold tabular-nums text-down">
                    {problems}
                  </span>
                )}
                <Link
                  href={namespaceHref(namespace)}
                  title="Namespace sayfası"
                  className="rounded-md px-1.5 py-0.5 tabular-nums hover:bg-sunken hover:text-ink"
                >
                  {group.length}
                </Link>
              </div>
              {open && (
                <ul>
                  {group.map((item) => (
                    <Row key={item.id} item={item} beats={beats} active={item.id === activeId} />
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

function Row({ item, beats, active }: { item: ListItem; beats: number; active: boolean }) {
  const style = stateStyles[item.state];
  return (
    <li>
      <Link
        href={`/monitors/${item.id}`}
        aria-current={active ? "page" : undefined}
        className={`flex items-center gap-2.5 rounded-lg px-2 py-2 ${
          active ? "bg-sunken" : "hover:bg-sunken/60"
        }`}
      >
        <span
          title={style.label}
          className={`w-14 shrink-0 rounded-full py-0.5 text-center text-xs font-semibold tabular-nums ${style.soft}`}
        >
          {pillText(item)}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm font-medium">{item.name}</span>
          <span className="block truncate text-xs text-muted">
            {item.kind ?? typeLabel(item.type)}
            {item.target && ` · ${item.target}`}
          </span>
        </span>
        <Beats beats={item.beats} slots={beats} />
      </Link>
    </li>
  );
}

/** The recent checks as small bars, oldest on the left. */
function Beats({ beats, slots }: { beats: string; slots: number }) {
  const shown = beats.slice(-slots);
  const failed = shown.split("0").length - 1;
  return (
    <span
      role="img"
      aria-label={`Son ${shown.length} kontrolün ${failed} tanesi başarısız`}
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
