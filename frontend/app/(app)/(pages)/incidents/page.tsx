import type { Metadata } from "next";
import Link from "next/link";
import { connection } from "next/server";
import { Suspense } from "react";
import { countIncidents, type Incident, queryIncidentsPage } from "@/lib/api";
import { config } from "@/lib/config";
import {
  csvHref,
  filterHref,
  filterToQuery,
  type IncidentFilter,
  isFiltered,
  parseIncidentFilter,
} from "@/lib/incident-filter";
import { getT } from "@/lib/i18n/server";
import { compareNamespaces } from "@/lib/namespaces";
import { workspaceMonitors } from "@/lib/workspace";
import { BackendError } from "@/app/ui/backend-error";
import { IncidentsIcon } from "@/app/ui/icons";
import { IncidentFilters, type MonitorOption } from "@/app/ui/incident-filters";
import { ChevronDownIcon, DownloadIcon } from "@/app/ui/incident-icons";
import { IncidentTable } from "@/app/ui/incident-table";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { monitorLabel } from "@/app/ui/monitor-name";

export async function generateMetadata(): Promise<Metadata> {
  const t = await getT();
  return { title: t("nav.incidents") };
}

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

/** When the request is answered: where "the last 24 hours" end. */
async function requestTime(): Promise<number> {
  await connection();
  return Date.now();
}

/** The namespaces and monitors the list can be narrowed to: those the viewer can see. */
async function filterOptions(): Promise<{ namespaces: string[]; monitors: MonitorOption[] }> {
  try {
    const monitors = await workspaceMonitors();
    const options = monitors
      .map((m) => ({ id: m.id, name: monitorLabel(m).name, namespace: m.namespace }))
      .sort((a, b) => compareNamespaces(a.namespace, b.namespace) || a.name.localeCompare(b.name));
    return { namespaces: [...new Set(options.map((m) => m.namespace))], monitors: options };
  } catch {
    // The list still works without the choices; the context bar says what is wrong.
    return { namespaces: [], monitors: [] };
  }
}

async function Incidents({ searchParams }: { searchParams: SearchParams }) {
  const filter = parseIncidentFilter(await searchParams, config.incidentRanges);
  const t = await getT();
  const now = await requestTime();
  const pageSize = config.incidentsLimit;
  // The counts of the state control are for the rest of the filter, whatever state it picks.
  const unstated = filterToQuery({ ...filter, state: null }, now);
  delete unstated.sort;
  delete unstated.order;

  let page: { incidents: Incident[]; total: number };
  let counts: { all: number; open: number; resolved: number };
  let options: Awaited<ReturnType<typeof filterOptions>>;
  try {
    const [listed, all, open, choices] = await Promise.all([
      queryIncidentsPage(filterToQuery(filter, now, pageSize)),
      filter.state === null ? null : countIncidents(unstated),
      filter.state === "open" ? null : countIncidents({ ...unstated, state: "open" }),
      filterOptions(),
    ]);
    page = listed;
    const allCount = all ?? listed.total;
    const openCount = open ?? listed.total;
    counts = { all: allCount, open: openCount, resolved: Math.max(0, allCount - openCount) };
    options = choices;
  } catch (err) {
    return (
      <>
        <PageHead csv={null} />
        <BackendError what={t("nav.incidents")} err={err} />
      </>
    );
  }

  const lastPage = Math.max(1, Math.ceil(page.total / pageSize));
  const first = (filter.page - 1) * pageSize + 1;
  const last = (filter.page - 1) * pageSize + page.incidents.length;

  return (
    <>
      <PageHead csv={page.total > 0 ? csvHref(filter) : null} />
      <IncidentFilters
        filter={filter}
        counts={counts}
        namespaces={options.namespaces}
        monitors={options.monitors}
        ranges={config.incidentRanges}
      >
        {page.incidents.length > 0 ? (
          <IncidentTable
            incidents={page.incidents}
            showMonitor
            sort={{
              by: filter.sort,
              order: filter.order,
              href: (sort, order) => filterHref(filter, { sort, ...(order && { order }) }),
            }}
            footer={
              <Pages
                filter={filter}
                total={page.total}
                first={first}
                last={last}
                hasPrev={filter.page > 1}
                hasNext={filter.page < lastPage}
              />
            }
          />
        ) : page.total > 0 ? (
          // Past the last page: the list got shorter since the link was made.
          <Empty
            title={t("incidents.past.title")}
            body={t.n("incidents.past.body", page.total, { count: page.total.toLocaleString(t.intl) })}
            action={
              <Link href={filterHref(filter, { page: lastPage })} className="btn">
                {t("incidents.past.last")}
              </Link>
            }
          />
        ) : isFiltered(filter) ? (
          <Empty
            title={t("incidents.none.title")}
            body={t("incidents.none.body")}
            action={
              <Link href="/incidents" className="btn">
                {t("incidents.clear")}
              </Link>
            }
          />
        ) : (
          <Empty title={t("incidents.empty.title")} body={t("incidents.empty.body")} />
        )}
      </IncidentFilters>
    </>
  );
}

/** The title, what the list holds, and the export of what it shows. */
async function PageHead({ csv }: { csv: string | null }) {
  const t = await getT();
  return (
    <header className="inc-head">
      <div className="min-w-0">
        <h1 className="type-display max-sm:type-display-s">{t("nav.incidents")}</h1>
        <p className="inc-lead">{t("incidents.intro")}</p>
      </div>
      {csv && (
        <a href={csv} className="btn btn-ghost" download title={t("incidents.csv.hint")}>
          <DownloadIcon />
          {t("incidents.csv")}
        </a>
      )}
    </header>
  );
}

/** Where the rows are in the whole list, and the way to the pages around. */
async function Pages({
  filter,
  total,
  first,
  last,
  hasPrev,
  hasNext,
}: {
  filter: IncidentFilter;
  total: number;
  first: number;
  last: number;
  hasPrev: boolean;
  hasNext: boolean;
}) {
  const t = await getT();
  const n = (v: number) => v.toLocaleString(t.intl);
  return (
    <nav className="inc-pages" aria-label={t("incidents.page.label")}>
      <p>
        {hasPrev || hasNext
          ? t("incidents.page.range", { from: n(first), to: n(last), total: t.n("incidents.page.total", total, { count: n(total) }) })
          : t.n("incidents.page.total", total, { count: n(total) })}
      </p>
      {(hasPrev || hasNext) && (
        <span className="inc-pages-links">
          {hasPrev ? (
            <Link href={filterHref(filter, { page: filter.page - 1 })} className="btn btn-sm" rel="prev">
              <ChevronDownIcon className="rotate-90" />
              {t("incidents.page.prev")}
            </Link>
          ) : (
            <span className="btn btn-sm" aria-disabled="true">
              <ChevronDownIcon className="rotate-90" />
              {t("incidents.page.prev")}
            </span>
          )}
          {hasNext ? (
            <Link href={filterHref(filter, { page: filter.page + 1 })} className="btn btn-sm" rel="next">
              {t("incidents.page.next")}
              <ChevronDownIcon className="-rotate-90" />
            </Link>
          ) : (
            <span className="btn btn-sm" aria-disabled="true">
              {t("incidents.page.next")}
              <ChevronDownIcon className="-rotate-90" />
            </span>
          )}
        </span>
      )}
    </nav>
  );
}

/** Nothing to list: a title, a sentence and what to do about it. */
function Empty({ title, body, action }: { title: string; body: string; action?: React.ReactNode }) {
  return (
    <div className="panel inc-empty">
      <span className="inc-empty-tile" aria-hidden>
        <IncidentsIcon width={20} height={20} />
      </span>
      <h2 className="type-title">{title}</h2>
      <p>{body}</p>
      {action}
    </div>
  );
}

export default async function Page({ searchParams }: PageProps<"/incidents">) {
  const t = await getT();
  return (
    <main className="page page-wide inc-list">
      <LiveRefresh />
      <Suspense
        fallback={
          <>
            <h1 className="type-display max-sm:type-display-s">{t("nav.incidents")}</h1>
            <p className="text-sm text-ink-3">{t("common.loading")}</p>
          </>
        }
      >
        <Incidents searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
