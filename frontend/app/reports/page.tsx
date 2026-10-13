import Link from "next/link";
import { Suspense } from "react";
import { getReport, type NamespaceReport, type Report } from "@/lib/api";
import { duration, uptime } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import { MANUAL_LABEL, namespaceHref } from "@/lib/namespaces";
import { typeLabel } from "@/lib/monitor-types";
import { BackendError } from "@/app/ui/backend-error";
import { LocalTime } from "@/app/ui/local-time";

const periods = [1, 7, 30, 90];

function uptimeClass(value: number | null): string {
  if (value === null) return "text-muted";
  if (value >= 99.9) return "text-up";
  if (value >= 99) return "text-warn";
  return "text-down";
}

async function NamespaceSection({ group }: { group: NamespaceReport }) {
  const t = await getT();
  return (
    <section className="card overflow-x-auto" aria-label={group.namespace || t(MANUAL_LABEL)}>
      <header className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-line px-4 py-3">
        <h2 className="font-semibold">
          <Link href={namespaceHref(group.namespace)} className="hover:underline">
            {group.namespace || t(MANUAL_LABEL)}
          </Link>
          <span className="ml-2 text-xs font-normal text-muted">
            {t.n("reports.namespace.monitors", group.monitors.length)}
          </span>
        </h2>
        <p className="text-sm">
          <span className={`font-semibold tabular-nums ${uptimeClass(group.uptime)}`}>{uptime(group.uptime, t)}</span>
          <span className="text-muted">
            {" "}
            · {t.n("reports.namespace.outages", group.outages)} ·{" "}
            {t("reports.namespace.total", { duration: duration(group.downtime_seconds, t) })}
          </span>
        </p>
      </header>
      <table className="w-full text-left text-sm">
        <thead className="text-xs text-muted">
          <tr>
            <th className="px-4 py-2 font-medium">{t("reports.column.monitor")}</th>
            <th className="px-4 py-2 text-right font-medium">{t("reports.column.uptime")}</th>
            <th className="px-4 py-2 text-right font-medium">{t("reports.column.checks")}</th>
            <th className="px-4 py-2 text-right font-medium">{t("reports.column.latency")}</th>
            <th className="px-4 py-2 text-right font-medium">{t("reports.column.outages")}</th>
            <th className="px-4 py-2 text-right font-medium">{t("reports.column.downtime")}</th>
            <th className="px-4 py-2 text-right font-medium">{t("reports.column.longest")}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {group.monitors.map((m) => (
            <tr key={m.id}>
              <td className="px-4 py-2">
                <Link href={`/monitors/${m.id}`} className="hover:underline">
                  {m.name}
                </Link>
                <span className="ml-2 text-xs text-muted">{t(typeLabel(m.type))}</span>
                {!m.enabled && <span className="ml-2 text-xs text-muted">{t("reports.monitor.paused")}</span>}
                {m.open_outage && (
                  <span className="ml-2 text-xs font-medium text-down">{t("reports.monitor.ongoing")}</span>
                )}
              </td>
              <td className={`px-4 py-2 text-right tabular-nums ${uptimeClass(m.uptime)}`}>{uptime(m.uptime, t)}</td>
              <td className="px-4 py-2 text-right tabular-nums text-muted">{m.checks}</td>
              <td className="px-4 py-2 text-right tabular-nums">{m.up_checks > 0 ? `${m.avg_latency_ms} ms` : "–"}</td>
              <td className={`px-4 py-2 text-right tabular-nums ${m.outages > 0 ? "text-down" : ""}`}>{m.outages}</td>
              <td className="px-4 py-2 text-right tabular-nums">{m.outages > 0 ? duration(m.downtime_seconds, t) : "–"}</td>
              <td className="px-4 py-2 text-right tabular-nums">
                {m.outages > 0 ? duration(m.longest_outage_seconds, t) : "–"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

async function Reports({ searchParams }: { searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const params = await searchParams;
  const requested = Number(typeof params.days === "string" ? params.days : 30);
  const days = periods.includes(requested) ? requested : 30;
  const namespace = typeof params.namespace === "string" ? params.namespace : undefined;
  const t = await getT();

  let report: Report;
  try {
    report = await getReport(days, namespace);
  } catch (err) {
    return <BackendError what={t("reports.what")} err={err} />;
  }
  const csv = `/reports/csv?days=${days}${namespace !== undefined ? `&namespace=${encodeURIComponent(namespace)}` : ""}`;

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <nav className="flex gap-1 rounded-lg bg-sunken p-1 text-sm" aria-label={t("reports.period")}>
          {periods.map((p) => (
            <Link
              key={p}
              href={`/reports?days=${p}${namespace !== undefined ? `&namespace=${encodeURIComponent(namespace)}` : ""}`}
              className={`rounded-md px-3 py-1 ${p === days ? "bg-panel font-medium shadow-sm" : "text-muted hover:text-ink"}`}
            >
              {p === 1 ? t("reports.period.day") : t.n("format.days", p)}
            </Link>
          ))}
        </nav>
        <p className="text-xs text-muted">
          <LocalTime iso={report.from} seconds={false} /> – <LocalTime iso={report.to} seconds={false} /> ·{" "}
          <a href={csv} className="underline hover:text-ink">
            {t("reports.csv")}
          </a>
        </p>
      </div>
      {report.namespaces.length === 0 ? (
        <p className="rounded-lg border border-dashed border-line p-8 text-center text-sm text-muted">
          {t("reports.empty")}
        </p>
      ) : (
        report.namespaces.map((g) => <NamespaceSection key={g.namespace} group={g} />)
      )}
    </>
  );
}

export default async function Page({ searchParams }: PageProps<"/reports">) {
  const t = await getT();
  return (
    <main className="mx-auto flex w-full max-w-5xl flex-col gap-5 px-4 py-6 sm:px-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("nav.reports")}</h1>
        <p className="mt-1 text-sm text-muted">{t("reports.intro")}</p>
      </div>
      <Suspense fallback={<p className="text-sm text-muted">{t("reports.calculating")}</p>}>
        <Reports searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
