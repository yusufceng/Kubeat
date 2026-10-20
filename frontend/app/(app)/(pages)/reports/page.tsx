import type { Metadata } from "next";
import Link from "next/link";
import { Suspense } from "react";
import { getReport, getStats, type MonitorReport, type NamespaceReport, type Report, type Stats } from "@/lib/api";
import { config } from "@/lib/config";
import { largestUnit, percentParts, uptimeTone } from "@/lib/figures";
import { milliseconds, span, uptime } from "@/lib/format";
import { typeWord } from "@/lib/incident-timeline";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { BackendError } from "@/app/ui/backend-error";
import { ReportsIcon } from "@/app/ui/icons";
import { CloseIcon, DownloadIcon } from "@/app/ui/incident-icons";
import { DateTime } from "@/app/ui/incident-time";
import { type Readout, Readouts } from "@/app/ui/readouts";
import { StatusGlyph } from "@/app/ui/status-glyph";

export async function generateMetadata(): Promise<Metadata> {
  const t = await getT();
  return { title: t("nav.reports") };
}

type SearchParams = Promise<Record<string, string | string[] | undefined>>;

/** The address of the report for a period, keeping the namespace it is narrowed to. */
function reportHref(days: number, namespace: string | undefined, path = "/reports"): string {
  const params = new URLSearchParams({ days: String(days) });
  if (namespace !== undefined) params.set("namespace", namespace);
  return `${path}?${params}`;
}

/** A percent as a readout's value, its sign set small where the language puts it: "99.73%", "%99,73". */
function Percent({ value, t }: { value: number; t: T }) {
  return (
    <>
      {percentParts(value, t.intl).map((p, i) =>
        p.sign ? (
          <small key={i} className="rep-sign">
            {p.text}
          </small>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  );
}

/** A period as a readout's value in its largest unit: 7,5 dk; 4,2 gün. */
function period(seconds: number, t: T): Pick<Readout, "value" | "unit"> {
  const { value, unit } = largestUnit(seconds);
  return { value: value.toLocaleString(t.intl), unit: t(`reports.unit.${unit}`) };
}

/** The period as a whole, in figures: from /stats, the same sums the table's rows come from. */
function figures(stats: Stats, namespaces: number, t: T): Readout[] {
  const n = (v: number) => v.toLocaleString(t.intl);
  const tone = uptimeTone(stats.uptime);
  return [
    {
      label: t("reports.readout.uptime"),
      value: stats.uptime === null ? "—" : <Percent value={stats.uptime} t={t} />,
      tone: tone ?? undefined,
      hint: t.n("reports.readout.uptime.hint", stats.checks, { count: n(stats.checks) }),
    },
    {
      label: t("reports.readout.outages"),
      glyph: stats.outages > 0 ? "down" : undefined,
      value: n(stats.outages),
      tone: stats.outages === 0 ? "muted" : undefined,
      hint:
        stats.outages === 0
          ? t("reports.readout.outages.none")
          : t("reports.namespace.total", { duration: span(stats.downtime_seconds * 1000, t) }),
    },
    {
      label: t("reports.mttr"),
      ...(stats.mttr_seconds === null ? { value: "—", tone: "muted" as const } : period(stats.mttr_seconds, t)),
      hint: t("reports.readout.mttr.hint"),
    },
    {
      label: t("reports.mtbf"),
      ...(stats.mtbf_seconds === null ? { value: "—", tone: "muted" as const } : period(stats.mtbf_seconds, t)),
      hint: t("reports.readout.mtbf.hint"),
    },
    {
      label: t("reports.readout.open"),
      glyph: stats.open_outages > 0 ? "down" : stats.open_incidents > 0 ? "warn" : undefined,
      value: n(stats.open_incidents),
      tone: stats.open_outages > 0 ? "down" : stats.open_incidents > 0 ? "warn" : "muted",
      hint: t("reports.readout.open.hint"),
    },
    {
      label: t("reports.readout.monitors"),
      value: n(stats.monitors),
      hint: t.n("reports.readout.monitors.hint", namespaces, { count: n(namespaces) }),
    },
  ];
}

/** An uptime as a table writes it, coloured only once it is not healthy. */
function UptimeCell({ value, t }: { value: number | null; t: T }) {
  const tone = uptimeTone(value);
  const color = tone === "down" ? "text-down-ink" : tone === "warn" ? "text-warn-ink" : tone === "muted" ? "text-ink-3" : "";
  return (
    <td role="cell" className={`rep-num rep-uptime ${color}`}>
      {uptime(value, t)}
    </td>
  );
}

/**
 * A figure of a row. label names it where a phone folds the row into lines;
 * a cell with nothing to say (no outage, so no length or MTTR) is a dash
 * here and is left out there.
 */
function Cell({
  label,
  none,
  className = "",
  children,
}: {
  label: string;
  none?: string;
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <td role="cell" data-label={label} className={`rep-num ${none !== undefined ? "rep-none text-ink-3" : ""} ${className}`} title={none}>
      {none !== undefined ? (
        <>
          <span aria-hidden>—</span>
          <span className="sr-only">{none}</span>
        </>
      ) : (
        children
      )}
    </td>
  );
}

/** What a row's figures are called, for a phone's folded rows. */
function labels(t: T) {
  return {
    outages: t("reports.column.outages"),
    downtime: t("reports.column.downtime"),
    longest: t("reports.column.longest"),
    mttr: t("reports.mttr"),
    mtbf: t("reports.mtbf"),
    latency: t("reports.column.latency"),
    checks: t("reports.column.checks"),
  };
}

/** The figures after the uptime, the same for a namespace and a monitor. */
function Figures({ r, longest, t }: { r: NamespaceReport | MonitorReport; longest: number | null; t: T }) {
  const n = (v: number) => v.toLocaleString(t.intl);
  const l = labels(t);
  const none = r.outages === 0 ? t("reports.noOutages") : undefined;
  return (
    <>
      <Cell label={l.outages} className={r.outages === 0 ? "text-ink-3 rep-none" : ""}>
        {n(r.outages)}
      </Cell>
      <Cell label={l.downtime} none={none}>
        {span(r.downtime_seconds * 1000, t)}
      </Cell>
      {longest === null ? (
        <td role="cell" className="rep-num rep-none" />
      ) : (
        <Cell label={l.longest} none={none}>
          {span(longest * 1000, t)}
        </Cell>
      )}
      <Cell label={l.mttr} none={r.mttr_seconds == null ? t("reports.noOutages") : undefined}>
        {span((r.mttr_seconds ?? 0) * 1000, t)}
      </Cell>
      <Cell label={l.mtbf} none={r.mtbf_seconds == null ? t("reports.noOutages") : undefined}>
        {span((r.mtbf_seconds ?? 0) * 1000, t)}
      </Cell>
    </>
  );
}

function MonitorRow({ m, t }: { m: MonitorReport; t: T }) {
  const n = (v: number) => v.toLocaleString(t.intl);
  const l = labels(t);
  return (
    // A monitor without outages has nothing for a phone's second line.
    <tr role="row" data-calm={m.outages === 0 || undefined}>
      <th role="rowheader" scope="row" className="rep-name">
        <span className="rep-name-line">
          {m.open_outage ? (
            <StatusGlyph kind="down" live label={t("reports.monitor.ongoing")} />
          ) : !m.enabled ? (
            <StatusGlyph kind="paused" label={t("reports.monitor.paused")} />
          ) : (
            <span className="rep-glyph-gap" aria-hidden />
          )}
          <Link href={`/monitors/${m.id}`} title={t("reports.monitor.open", { name: m.name })}>
            {m.name}
          </Link>
          <span className="rep-type">{typeWord(m.type)}</span>
        </span>
      </th>
      <UptimeCell value={m.uptime} t={t} />
      <Figures r={m} longest={m.longest_outage_seconds} t={t} />
      <Cell label={l.latency} className="rep-wide" none={m.up_checks > 0 ? undefined : t("common.none")}>
        {milliseconds(m.avg_latency_ms, t)}
      </Cell>
      <Cell label={l.checks} className="rep-wide text-ink-3">
        {n(m.checks)}
      </Cell>
    </tr>
  );
}

function NamespaceRows({ group, t }: { group: NamespaceReport; t: T }) {
  const n = (v: number) => v.toLocaleString(t.intl);
  const name = group.namespace || t(MANUAL_LABEL);
  return (
    <tbody role="rowgroup">
      <tr role="row" className="rep-ns" data-calm={group.outages === 0 || undefined}>
        <th role="rowheader" scope="rowgroup" className="rep-name">
          <span className="rep-name-line">
            <Link
              href={namespaceHref(group.namespace)}
              className={group.namespace ? "rep-ns-name" : "rep-ns-name rep-ns-manual"}
              title={t("reports.namespace.open", { namespace: name })}
            >
              {name}
            </Link>
            <span className="rep-count">{t.n("reports.namespace.monitors", group.monitors.length, { count: n(group.monitors.length) })}</span>
          </span>
        </th>
        <UptimeCell value={group.uptime} t={t} />
        {/* A namespace's longest outage is its worst monitor's, in the rows below. */}
        <Figures r={group} longest={null} t={t} />
        <td role="cell" className="rep-num rep-wide rep-none" />
        <td role="cell" className="rep-num rep-wide rep-none" />
      </tr>
      {group.monitors.map((m) => (
        <MonitorRow key={m.id} m={m} t={t} />
      ))}
    </tbody>
  );
}

async function Reports({ searchParams }: { searchParams: SearchParams }) {
  const params = await searchParams;
  const periods = config.reportPeriods;
  const requested = Number(typeof params.days === "string" ? params.days : NaN);
  const days = periods.includes(requested) ? requested : periods[0];
  const namespace = typeof params.namespace === "string" ? params.namespace : undefined;
  const t = await getT();

  let report: Report;
  let stats: Stats | null;
  try {
    const [r, s] = await Promise.all([
      getReport(days, namespace),
      // The figures of the whole are a help, not the report: without them the table still stands.
      getStats(days, namespace !== undefined ? [namespace] : undefined).catch(() => null),
    ]);
    report = r;
    stats = s;
  } catch (err) {
    return <BackendError what={t("reports.what")} err={err} />;
  }
  const empty = report.namespaces.length === 0;

  return (
    <>
      <div className="rep-bar">
        <nav className="inc-seg" aria-label={t("reports.period")}>
          {[...periods]
            .sort((a, b) => a - b)
            .map((p) => (
              <Link key={p} href={reportHref(p, namespace)} aria-current={p === days ? "page" : undefined} scroll={false}>
                {p === 1 ? t("reports.period.day") : t.n("format.days", p)}
              </Link>
            ))}
        </nav>
        {namespace !== undefined && (
          <span className="rep-filter">
            <span>{t("reports.namespace.only", { namespace: namespace || t(MANUAL_LABEL) })}</span>
            <Link
              href={reportHref(days, undefined)}
              className="rep-filter-x"
              aria-label={t("reports.namespace.clear")}
              title={t("reports.namespace.clear")}
            >
              <CloseIcon width={14} height={14} />
            </Link>
          </span>
        )}
        <p className="rep-span">
          <DateTime iso={report.from} /> – <DateTime iso={report.to} />
          <span aria-hidden> · </span>
          {t("reports.generated")}
        </p>
        {!empty && (
          <a href={reportHref(days, namespace, "/reports/csv")} className="btn btn-ghost rep-csv" download>
            <DownloadIcon />
            {t("reports.csv")}
          </a>
        )}
      </div>
      {/* Figures of nothing say nothing: an empty report shows only that it is empty. */}
      {!empty && stats && <Readouts items={figures(stats, report.namespaces.length, t)} label={t("reports.readouts")} />}
      {!empty && (
        <dl className="rep-defs">
          <div>
            <dt>
              <abbr title={t("reports.mttr.name")}>{t("reports.mttr")}</abbr>
              <span>{t("reports.mttr.name")}:</span>
            </dt>
            <dd>{t("reports.mttr.means")}</dd>
          </div>
          <div>
            <dt>
              <abbr title={t("reports.mtbf.name")}>{t("reports.mtbf")}</abbr>
              <span>{t("reports.mtbf.name")}:</span>
            </dt>
            <dd>{t("reports.mtbf.means")}</dd>
          </div>
        </dl>
      )}
      {empty ? (
        <div className="panel inc-empty">
          <span className="inc-empty-tile" aria-hidden>
            <ReportsIcon width={20} height={20} />
          </span>
          <h2 className="type-title">{t("reports.empty")}</h2>
          {namespace !== undefined && (
            <Link href={reportHref(days, undefined)} className="btn">
              {t("reports.namespace.clear")}
            </Link>
          )}
        </div>
      ) : (
        <div className="panel rep-panel">
          <table role="table" className="rep-table" aria-label={t("reports.table")}>
            <thead role="rowgroup">
              <tr role="row">
                <th role="columnheader" scope="col" className="rep-name">
                  {t("reports.column.monitor")}
                </th>
                <th role="columnheader" scope="col">{t("reports.column.uptime")}</th>
                <th role="columnheader" scope="col">{t("reports.column.outages")}</th>
                <th role="columnheader" scope="col">{t("reports.column.downtime")}</th>
                <th role="columnheader" scope="col">{t("reports.column.longest")}</th>
                <th role="columnheader" scope="col">
                  <abbr title={t("reports.mttr.name")}>{t("reports.mttr")}</abbr>
                </th>
                <th role="columnheader" scope="col">
                  <abbr title={t("reports.mtbf.name")}>{t("reports.mtbf")}</abbr>
                </th>
                <th role="columnheader" scope="col" className="rep-wide">
                  {t("reports.column.latency")}
                </th>
                <th role="columnheader" scope="col" className="rep-wide">
                  {t("reports.column.checks")}
                </th>
              </tr>
            </thead>
            {report.namespaces.map((g) => (
              <NamespaceRows key={g.namespace} group={g} t={t} />
            ))}
          </table>
        </div>
      )}
    </>
  );
}

export default async function Page({ searchParams }: PageProps<"/reports">) {
  const t = await getT();
  return (
    <main className="page rep-page">
      <header>
        <h1 className="type-display max-sm:type-display-s">{t("nav.reports")}</h1>
        <p className="inc-lead">{t("reports.lead")}</p>
      </header>
      <Suspense fallback={<p className="text-sm text-ink-3">{t("reports.calculating")}</p>}>
        <Reports searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
