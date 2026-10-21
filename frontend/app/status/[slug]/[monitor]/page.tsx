import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { connection } from "next/server";
import { cache, Suspense } from "react";
import { ApiError, getStatusPageMonitor, type StatusPageLocked, type StatusPageMonitorDetail } from "@/lib/api";
import { config } from "@/lib/config";
import { duration, span } from "@/lib/format";
import { I18nProvider } from "@/lib/i18n/client";
import { getT } from "@/lib/i18n/server";
import { translator } from "@/lib/i18n/translate";
import { wallScript } from "@/lib/status-page";
import { BeatStrip } from "@/app/ui/beat-strip";
import { ChevronIcon } from "@/app/ui/icons";
import { DateTime } from "@/app/ui/incident-time";
import { LatencyChart } from "@/app/ui/latency-chart";
import { LiveDuration } from "@/app/ui/live-duration";
import { RefreshCountdown } from "@/app/ui/refresh-countdown";
import { StatusGlyph } from "@/app/ui/status-glyph";
import { SystemTheme } from "@/app/ui/theme-toggle";
import { statusPagePath, statusPageToken } from "../access";
import { dayBeats, Footer, NARROW_DAYS, Skeleton, StateWords, Unavailable, UptimeWindows } from "../shared";
import { UnlockForm, unlockProblem } from "../unlock-form";
import { WallToggle } from "../wall";

/** The monitor's id from its path segment: a whole positive number, or nothing. */
function monitorId(segment: string): number | null {
  return /^[1-9]\d*$/.test(segment) ? Number(segment) : null;
}

/** The detail, read once per request for both the page and its title. */
const readDetail = cache(async (slug: string, id: number) => getStatusPageMonitor(slug, id, await statusPageToken(slug)));

function locked(err: unknown): StatusPageLocked | null {
  if (err instanceof ApiError && err.status === 401 && typeof err.body === "object" && err.body !== null && (err.body as StatusPageLocked).locked) {
    return err.body as StatusPageLocked;
  }
  return null;
}

export async function generateMetadata({ params }: PageProps<"/status/[slug]/[monitor]">): Promise<Metadata> {
  const { slug, monitor } = await params;
  const id = monitorId(monitor);
  if (id === null) return {};
  try {
    const detail = await readDetail(slug, id);
    return { title: { absolute: `${detail.monitor.name} · ${detail.title}` }, ...(detail.noindex && { robots: { index: false } }) };
  } catch (err) {
    const lock = locked(err);
    return lock ? { title: { absolute: lock.title }, robots: { index: false } } : {};
  }
}

async function Detail({
  params,
  searchParams,
}: {
  params: Promise<{ slug: string; monitor: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  await connection();
  const { slug, monitor } = await params;
  const id = monitorId(monitor);
  if (id === null) notFound();
  const tools = (
    <div className="sp-head-tools">
      <RefreshCountdown intervalMs={config.statusPageRefreshMs} />
      <WallToggle />
    </div>
  );

  let detail: StatusPageMonitorDetail;
  try {
    detail = await readDetail(slug, id);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    const visitor = await getT();
    const lock = locked(err);
    if (lock) {
      const query = await searchParams;
      return <UnlockForm slug={slug} title={lock.title} next={`${statusPagePath(slug)}/${id}`} problem={unlockProblem(query.unlock)} t={visitor} />;
    }
    return <Unavailable t={visitor} tools={tools} />;
  }

  const t = translator(detail.language);
  const m = detail.monitor;
  const daysAgo = (days: number) => t.n("statusPage.daysAgo", days);
  // The outages as bands on the chart; one still open runs to its end.
  const bands = detail.incidents.map((i) => ({ from: Date.parse(i.started_at), to: i.resolved_at ? Date.parse(i.resolved_at) : null }));
  return (
    <I18nProvider locale={detail.language}>
      {/* The document's lang is the visitor's until the browser takes over; the page says its own here. */}
      <div lang={detail.language} className="contents">
      <header className="sp-head">
        <div className="sp-head-main">
          <p className="sp-back">
            <Link href={statusPagePath(slug)} className="sp-back-link">
              <ChevronIcon className="rotate-180" width={14} height={14} />
              {detail.title}
            </Link>
          </p>
          <h1 className="sp-title">{m.name}</h1>
          <StateWords state={m.state} t={t} size={12} className="sp-detail-state" />
        </div>
        {tools}
      </header>

      <section aria-label={t("statusPage.windows.aria")} className="mt-6">
        <UptimeWindows windows={m.uptime} label={t("statusPage.windows.aria")} t={t} className="panel sp-windows-panel" />
      </section>

      <section aria-labelledby="sp-days">
        <h2 id="sp-days" className="sp-section-title">
          {t("statusPage.detail.days", { days: detail.days })}
        </h2>
        <div className="panel sp-days">
          <BeatStrip
            beats={dayBeats(m.history, new Date(detail.from).getTime(), t)}
            slots={detail.days}
            narrowSlots={NARROW_DAYS}
            height={36}
            label={t.n("statusPage.history.aria", m.history.length)}
          />
          <div className="sp-axis">
            <span className="max-sm:hidden">{daysAgo(detail.days)}</span>
            <span className="sm:hidden">{daysAgo(Math.min(detail.days, NARROW_DAYS))}</span>
            <span>{t("statusPage.today")}</span>
          </div>
        </div>
      </section>

      <section aria-labelledby="sp-latency">
        <h2 id="sp-latency" className="sp-section-title">
          {t("statusPage.detail.latency", { range: duration(detail.series.range_seconds, t) })}
        </h2>
        <div className="panel sp-chart">
          <LatencyChart series={detail.series} baseHref="" selection={null} incidents={bands} readOnly />
        </div>
      </section>

      <section aria-labelledby="sp-outages">
        <h2 id="sp-outages" className="sp-section-title">
          {t("statusPage.detail.incidents", { days: detail.days })}
        </h2>
        {detail.incidents.length === 0 ? (
          <p className="panel sp-calm">
            <StatusGlyph kind="up" />
            {t("statusPage.detail.noIncidents", { days: detail.days })}
          </p>
        ) : (
          <div className="panel sp-table-panel">
            <table className="sp-table">
              <thead>
                <tr>
                  <th scope="col">{t("statusPage.detail.col.start")}</th>
                  <th scope="col">{t("statusPage.detail.col.end")}</th>
                  <th scope="col" className="num">
                    {t("statusPage.detail.col.duration")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {detail.incidents.map((i) => (
                  <tr key={i.started_at}>
                    <td>
                      <span className="sp-table-glyph">
                        <StatusGlyph kind={i.resolved_at ? "up" : "down"} live={!i.resolved_at} />
                        <DateTime iso={i.started_at} />
                      </span>
                    </td>
                    <td>{i.resolved_at ? <DateTime iso={i.resolved_at} /> : <span className="font-semibold text-down-ink">{t("statusPage.incidents.ongoing")}</span>}</td>
                    <td className="num">{i.resolved_at ? span(i.duration_seconds * 1000, t) : <LiveDuration since={i.started_at} />}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <Footer t={t} />
      </div>
    </I18nProvider>
  );
}

/**
 * One service of a published page on a page of its own: its state, its
 * uptime over each window, its days, its response times of late, and
 * when its outages began and ended. Nothing about why or where.
 */
export default async function Page({ params, searchParams }: PageProps<"/status/[slug]/[monitor]">) {
  const t = await getT();
  return (
    <main className="sp-page">
      <script dangerouslySetInnerHTML={{ __html: wallScript }} />
      <SystemTheme />
      <Suspense
        fallback={
          <>
            <Skeleton label={t("common.loading")} />
            <Footer t={t} />
          </>
        }
      >
        <Detail params={params} searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
