import Link from "next/link";
import type { Incident } from "@/lib/api";
import { gatherRuns, runExtent } from "@/lib/flaps";
import { span } from "@/lib/format";
import type { IncidentSort } from "@/lib/incident-filter";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { MANUAL_LABEL } from "@/lib/namespaces";
import { ArrowDownIcon, ArrowUpIcon, SortIcon } from "@/app/ui/incident-icons";
import { CompactTime } from "@/app/ui/incident-time";
import { LiveDuration } from "@/app/ui/live-duration";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** How the list is sorted, and the address of each way of sorting it. */
export type TableSort = {
  by: IncidentSort;
  order: "asc" | "desc";
  /** The list sorted by a column: in its own direction, or the other way round if it is the current one. */
  href: (by: IncidentSort, order?: "asc" | "desc") => string;
};

/** An incident's glyph and what it says: an outage still going on pulses, a closed one is healthy again. */
export function incidentGlyph(i: Pick<Incident, "kind" | "resolved_at">, t: T) {
  if (i.resolved_at) return { kind: "up" as const, live: false, label: t("incidents.state.closed") };
  if (i.kind === "cert") return { kind: "warn" as const, live: false, label: t("incidents.state.cert.open") };
  return { kind: "down" as const, live: true, label: t("incidents.state.down.open") };
}

/** A run of short outages of one monitor, as the table gathers it (see lib/flaps.ts). */
export type RunLink = (run: { monitorId: number; from: string; to: string }) => string;

/**
 * Incidents as a table of 36px rows, each opening the incident's page: the
 * state as a glyph, the monitor and its namespace, when it began, how long
 * it lasted (counting while it goes on) and its cause. Names never wrap;
 * what does not fit ends in an ellipsis with the whole in its title. With
 * sort, the monitor, start and duration headings sort the list. With runs,
 * a monitor's run of short outages is one row, which opens the list of
 * them. On a phone each row folds into a short block of three lines.
 */
export async function IncidentTable({
  incidents,
  showMonitor,
  names,
  sort,
  runs,
  footer,
}: {
  incidents: Incident[];
  /** Off where the monitor is known (its own page): the kind is shown instead. */
  showMonitor: boolean;
  /** The monitors' names as the app shows them (see monitorLabel), by id; a monitor not in it keeps the incident's. */
  names?: Map<number, string>;
  sort?: TableSort;
  /** Gathers runs of short outages shorter than windowMs, each into a row that leads to href. */
  runs?: { windowMs: number; href: RunLink };
  /** Under the rows, inside the panel: the pages of a long list. */
  footer?: React.ReactNode;
}) {
  const t = await getT();
  const rows = runs ? gatherRuns(incidents, runs.windowMs) : incidents;
  const nameOf = (i: Incident) => names?.get(i.monitor_id) ?? i.monitor_name;

  const heading = (label: string, by: IncidentSort | null, className = "") => {
    if (!sort || !by) {
      return (
        <th role="columnheader" scope="col" className={className}>
          {label}
        </th>
      );
    }
    const active = sort.by === by;
    const Arrow = !active ? SortIcon : sort.order === "asc" ? ArrowUpIcon : ArrowDownIcon;
    return (
      <th
        role="columnheader"
        scope="col"
        className={className}
        aria-sort={active ? (sort.order === "asc" ? "ascending" : "descending") : undefined}
      >
        <Link
          href={active ? sort.href(by, sort.order === "asc" ? "desc" : "asc") : sort.href(by)}
          className="inc-sort"
          data-active={active || undefined}
          title={t("incidents.sortBy", { column: label })}
          scroll={false}
        >
          {label}
          <Arrow width={13} height={13} />
        </Link>
      </th>
    );
  };

  return (
    <div className="panel inc-panel">
      <table role="table" className={`inc-table ${showMonitor ? "" : "inc-table-own"}`} aria-label={t("incidents.table")}>
        <thead role="rowgroup">
          <tr role="row">
            {showMonitor ? (
              <>
                {heading(t("incidents.col.monitor"), "monitor", "inc-c-name")}
                <th role="columnheader" scope="col" className="inc-c-ns">
                  {t("incidents.col.namespace")}
                </th>
              </>
            ) : (
              heading(t("incidents.col.kind"), null, "inc-c-name")
            )}
            {heading(t("incidents.col.started"), "started_at", "inc-c-start")}
            {heading(t("incidents.col.duration"), "duration", "inc-c-dur")}
            <th role="columnheader" scope="col" className="inc-c-cause">
              {t("incidents.col.cause")}
            </th>
          </tr>
        </thead>
        <tbody role="rowgroup">
          {rows.map((row) => {
            if (Array.isArray(row)) return <RunRow key={`run-${row[0].id}`} run={row} name={nameOf(row[0])} href={runs!.href} t={t} />;
            const i = row;
            const glyph = incidentGlyph(i, t);
            const open = !i.resolved_at;
            const name = showMonitor ? nameOf(i) : t(i.kind === "cert" ? "incident.kind.cert" : "incident.kind.down");
            return (
              <tr role="row" key={i.id} data-open={open || undefined}>
                <td role="cell" className="inc-c-name">
                  <StatusGlyph kind={glyph.kind} live={glyph.live} label={glyph.label} />
                  <Link href={`/incidents/${i.id}`} className="inc-row-link" title={name}>
                    {name}
                  </Link>
                  {showMonitor && i.kind === "cert" && <span className="inc-tag">{t("incidents.certTag")}</span>}
                </td>
                {showMonitor && <NamespaceCell namespace={i.monitor_namespace} t={t} />}
                <td role="cell" className="inc-c-start">
                  <CompactTime iso={i.started_at} />
                </td>
                <td
                  role="cell"
                  className="inc-c-dur"
                  style={open ? { color: i.kind === "cert" ? "var(--warn-ink)" : "var(--down-ink)" } : undefined}
                >
                  {open ? (
                    <LiveDuration since={i.started_at} />
                  ) : (
                    span(Date.parse(i.resolved_at!) - Date.parse(i.started_at), t)
                  )}
                </td>
                <td role="cell" className="inc-c-cause">
                  <span title={i.cause}>{i.cause || t("common.none")}</span>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {footer}
    </div>
  );
}

function NamespaceCell({ namespace, t }: { namespace?: string; t: T }) {
  return (
    <td role="cell" className="inc-c-ns">
      {namespace ? (
        <span className="inc-mono" title={namespace}>
          {namespace}
        </span>
      ) : (
        <span className="inc-manual">{t(MANUAL_LABEL)}</span>
      )}
    </td>
  );
}

/**
 * A monitor's run of short outages as one row: how many, over when, how
 * long each lasted at least and at most, and the newest cause. It opens
 * the list narrowed to them, where they are told one by one.
 */
function RunRow({ run, name, href, t }: { run: Incident[]; name: string; href: RunLink; t: T }) {
  const extent = runExtent(run);
  const newest = run.reduce((a, b) => (a.started_at > b.started_at ? a : b));
  const lasted =
    extent.shortest === extent.longest ? span(extent.longest, t) : `${span(extent.shortest, t)}–${span(extent.longest, t)}`;
  const label = t.n("incidents.run.count", extent.count);
  return (
    <tr role="row" data-run>
      <td role="cell" className="inc-c-name">
        <StatusGlyph kind="warn" label={t("incidents.run.state")} />
        <Link
          href={href({ monitorId: newest.monitor_id, from: extent.from, to: extent.to })}
          className="inc-row-link"
          title={t("incidents.run.open", { name, outages: label })}
        >
          {name}
        </Link>
        <span className="inc-tag">{label}</span>
      </td>
      <NamespaceCell namespace={newest.monitor_namespace} t={t} />
      <td role="cell" className="inc-c-start">
        <CompactTime iso={extent.from} />–<CompactTime iso={extent.to} />
      </td>
      <td role="cell" className="inc-c-dur">
        {lasted}
      </td>
      <td role="cell" className="inc-c-cause">
        <span title={newest.cause}>{newest.cause || t("common.none")}</span>
      </td>
    </tr>
  );
}
