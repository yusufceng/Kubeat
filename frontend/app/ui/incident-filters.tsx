"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useId, useRef, useState, useTransition } from "react";
import { filterHref, type IncidentFilter, isFiltered, QUERY_MAX, rangeKey } from "@/lib/incident-filter";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { MANUAL_LABEL } from "@/lib/namespaces";
import { useMounted } from "@/lib/use-mounted";
import { SearchIcon } from "@/app/ui/icons";
import { CloseIcon, FilterIcon } from "@/app/ui/incident-icons";

/** A click that follows a link in place, not into a new tab or window. */
function plainClick(e: React.MouseEvent): boolean {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey;
}

/** A monitor the list can be narrowed to. */
export type MonitorOption = { id: number; name: string; namespace: string };

/** A period in words: "24 saat", "7 gün". */
export function periodWords(seconds: number, t: T): string {
  if (seconds % 86400 === 0 && seconds >= 2 * 86400) return t.n("incidents.range.days", seconds / 86400);
  if (seconds % 3600 === 0) return t.n("incidents.range.hours", seconds / 3600);
  return t.n("incidents.range.minutes", Math.round(seconds / 60));
}

/** A moment's calendar day in the viewer's zone, as a date input wants it. */
function localDate(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** The start of a calendar day in the viewer's zone, days later. */
function dayStart(date: string, days = 0): string {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(y, m - 1, d + days).toISOString();
}

/**
 * What narrows the incident list, kept in the address: the monitor's name,
 * the state with the count of each, and the namespace, monitor, kind and
 * period. A choice applies at once; the text after a pause in typing or on
 * Enter. The list it governs (children) dims while the next one loads.
 */
export function IncidentFilters({
  filter,
  counts,
  namespaces,
  monitors,
  ranges,
  children,
}: {
  filter: IncidentFilter;
  counts: { all: number; open: number; resolved: number };
  /** Every namespace the viewer can see; "" stands for the monitors without one. */
  namespaces: string[];
  monitors: MonitorOption[];
  /** The periods offered, in seconds. */
  ranges: number[];
  children: React.ReactNode;
}) {
  const t = useT();
  const router = useRouter();
  const mounted = useMounted();
  const [pending, startTransition] = useTransition();
  const [q, setQ] = useState(filter.q);
  const [custom, setCustom] = useState(filter.range === null && (filter.from !== null || filter.to !== null));
  const [more, setMore] = useState(false);
  const search = useRef<HTMLInputElement>(null);
  const ids = {
    q: useId(),
    selects: useId(),
    ns: useId(),
    monitor: useId(),
    kind: useId(),
    range: useId(),
    from: useId(),
    to: useId(),
  };
  // The parts of the filter behind the phone's button that are in use.
  const narrowing = [
    filter.namespaces.length > 0,
    filter.monitorId !== null,
    filter.kind !== null,
    filter.range !== null || filter.from !== null || filter.to !== null,
  ].filter(Boolean).length;

  const go = (changes: Partial<IncidentFilter>, replace = false) =>
    startTransition(() => {
      const href = filterHref(filter, changes);
      if (replace) router.replace(href, { scroll: false });
      else router.push(href, { scroll: false });
    });

  // The text applies after a pause in typing; what the address says wins
  // when it changes from elsewhere (back, a link).
  const [lastQ, setLastQ] = useState(filter.q);
  if (filter.q !== lastQ) {
    setLastQ(filter.q);
    setQ(filter.q);
  }
  useEffect(() => {
    if (q.trim() === filter.q) return;
    const id = setTimeout(() => go({ q: q.trim() }, true), 400);
    return () => clearTimeout(id);
    // go is a new function each render; the text is what matters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q, filter.q]);

  // "/" goes to the search field, as in the monitor list.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey) return;
      const target = e.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable=true]")) return;
      e.preventDefault();
      search.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const nsValue = filter.namespaces.length === 0 ? "*" : filter.namespaces.length === 1 ? `ns:${filter.namespaces[0]}` : "several";
  const rangeValue = filter.range !== null ? rangeKey(filter.range) : custom ? "custom" : "";
  const knownMonitor = filter.monitorId === null || monitors.some((m) => m.id === filter.monitorId);
  const byNamespace = new Map<string, MonitorOption[]>();
  for (const m of monitors) byNamespace.set(m.namespace, [...(byNamespace.get(m.namespace) ?? []), m]);

  const states: { key: IncidentFilter["state"]; label: string; count: number }[] = [
    { key: null, label: t("incidents.state.all"), count: counts.all },
    { key: "open", label: t("incidents.state.open"), count: counts.open },
    { key: "resolved", label: t("incidents.state.resolved"), count: counts.resolved },
  ];

  function applyDates(form: HTMLFormElement) {
    const data = new FormData(form);
    let from = String(data.get("from") ?? "");
    let to = String(data.get("to") ?? "");
    // Dates given the wrong way round are meant the right way round.
    if (from && to && from > to) [from, to] = [to, from];
    // The end is a whole day: up to the start of the next one.
    go({ range: null, from: from ? dayStart(from) : null, to: to ? dayStart(to, 1) : null });
  }

  return (
    <div className="inc-list-body" aria-busy={pending || undefined}>
      <div role="search" className="inc-filters" aria-label={t("incidents.filters")}>
        <div className="inc-filters-row">
          <form
            className="inc-search"
            onSubmit={(e) => {
              e.preventDefault();
              go({ q: q.trim() });
            }}
          >
            <label htmlFor={ids.q} className="sr-only">
              {t("incidents.search.label")}
            </label>
            <SearchIcon aria-hidden className="inc-search-icon" />
            <input
              ref={search}
              id={ids.q}
              name="q"
              type="search"
              className="field"
              placeholder={t("incidents.search")}
              value={q}
              maxLength={QUERY_MAX}
              autoComplete="off"
              onChange={(e) => setQ(e.target.value)}
            />
            <kbd className="kbd inc-search-kbd" aria-hidden>
              /
            </kbd>
          </form>
          <nav className="inc-seg" aria-label={t("incidents.state")}>
            {states.map((s) => (
              <Link
                key={s.key ?? "all"}
                href={filterHref(filter, { state: s.key })}
                aria-current={filter.state === s.key ? "page" : undefined}
                scroll={false}
                onClick={(e) => {
                  if (!plainClick(e)) return;
                  e.preventDefault();
                  go({ state: s.key });
                }}
              >
                {s.label}
                <b className={s.key === "open" && s.count > 0 ? "text-down-ink" : undefined}>{s.count.toLocaleString(t.intl)}</b>
              </Link>
            ))}
          </nav>
          {/* On a phone the rest of the filter waits behind this, with the number of its parts in use. */}
          <button
            type="button"
            className="btn inc-more"
            aria-expanded={more}
            aria-controls={ids.selects}
            aria-label={t("incidents.filters")}
            title={t("incidents.filters")}
            onClick={() => setMore((open) => !open)}
          >
            <FilterIcon />
            {narrowing > 0 && <b>{narrowing}</b>}
          </button>
        </div>
        <div id={ids.selects} className={`inc-filters-row inc-selects ${more ? "" : "max-sm:hidden"}`}>
          <span className="inc-select">
            <label htmlFor={ids.ns}>{t("incidents.namespace")}</label>
            <select
              id={ids.ns}
              className="field"
              value={nsValue}
              onChange={(e) => {
                const v = e.target.value;
                go({ namespaces: v === "*" ? [] : [v.slice(3)], monitorId: null });
              }}
            >
              <option value="*">{t("incidents.namespace.all")}</option>
              {nsValue === "several" && (
                <option value="several" disabled>
                  {t.n("incidents.namespace.several", filter.namespaces.length)}
                </option>
              )}
              {namespaces.map((ns) => (
                <option key={ns} value={`ns:${ns}`}>
                  {ns || t(MANUAL_LABEL)}
                </option>
              ))}
            </select>
          </span>
          <span className="inc-select">
            <label htmlFor={ids.monitor}>{t("incidents.monitor")}</label>
            <select
              id={ids.monitor}
              className="field"
              value={filter.monitorId ?? ""}
              onChange={(e) => go({ monitorId: e.target.value ? Number(e.target.value) : null })}
            >
              <option value="">{t("incidents.monitor.all")}</option>
              {!knownMonitor && <option value={filter.monitorId!}>{t("incidents.monitor.gone", { id: filter.monitorId! })}</option>}
              {[...byNamespace.entries()].map(([ns, list]) => (
                <optgroup key={ns} label={ns || t(MANUAL_LABEL)}>
                  {list.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.name}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </span>
          <span className="inc-select">
            <label htmlFor={ids.kind}>{t("incidents.kind")}</label>
            <select
              id={ids.kind}
              className="field"
              value={filter.kind ?? ""}
              onChange={(e) => go({ kind: (e.target.value || null) as IncidentFilter["kind"] })}
            >
              <option value="">{t("incidents.kind.all")}</option>
              <option value="down">{t("incidents.kind.down")}</option>
              <option value="cert">{t("incidents.kind.cert")}</option>
            </select>
          </span>
          <span className="inc-select">
            <label htmlFor={ids.range}>{t("incidents.range")}</label>
            <select
              id={ids.range}
              className="field"
              value={rangeValue}
              onChange={(e) => {
                const v = e.target.value;
                if (v === "custom") {
                  setCustom(true);
                  return;
                }
                setCustom(false);
                const range = ranges.find((r) => rangeKey(r) === v) ?? null;
                go({ range, from: null, to: null });
              }}
            >
              <option value="">{t("incidents.range.all")}</option>
              {ranges.map((r) => (
                <option key={r} value={rangeKey(r)}>
                  {t("incidents.range.last", { period: periodWords(r, t) })}
                </option>
              ))}
              <option value="custom">{t("incidents.range.custom")}</option>
            </select>
          </span>
          {custom && (
            <form
              className="inc-dates"
              onSubmit={(e) => {
                e.preventDefault();
                applyDates(e.currentTarget);
              }}
            >
              <span className="inc-select">
                <label htmlFor={ids.from}>{t("incidents.range.from")}</label>
                {/* The dates are the viewer's own days, which only the browser knows. */}
                <input
                  key={`from-${mounted}`}
                  id={ids.from}
                  name="from"
                  type="date"
                  className="field"
                  defaultValue={mounted ? localDate(filter.from) : ""}
                />
              </span>
              <span className="inc-select">
                <label htmlFor={ids.to}>{t("incidents.range.to")}</label>
                <input
                  key={`to-${mounted}`}
                  id={ids.to}
                  name="to"
                  type="date"
                  className="field"
                  // The address holds the start of the day after; the field shows the last day itself.
                  defaultValue={mounted && filter.to ? localDate(new Date(Date.parse(filter.to) - 1).toISOString()) : ""}
                />
              </span>
              <button type="submit" className="btn">
                {t("incidents.range.apply")}
              </button>
            </form>
          )}
          {isFiltered(filter) && (
            <Link
              href="/incidents"
              className="btn btn-ghost inc-clear"
              scroll={false}
              onClick={(e) => {
                if (!plainClick(e)) return;
                e.preventDefault();
                setQ("");
                setCustom(false);
                startTransition(() => router.push("/incidents", { scroll: false }));
              }}
            >
              <CloseIcon />
              {t("incidents.clear")}
            </Link>
          )}
        </div>
        {custom && <p className="inc-hint">{t("incidents.range.hint")}</p>}
      </div>
      <div className="inc-results">{children}</div>
    </div>
  );
}
