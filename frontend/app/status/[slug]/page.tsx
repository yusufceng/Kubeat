import { notFound } from "next/navigation";
import { connection } from "next/server";
import { Suspense } from "react";
import { ApiError, getStatusPageView, type PublicState, type StatusPageView } from "@/lib/api";
import { span, uptime } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import type { T } from "@/lib/i18n/translate";
import { PulseIcon } from "@/app/ui/icons";
import { LiveRefresh } from "@/app/ui/live-refresh";
import { LocalTime } from "@/app/ui/local-time";

const DAY_MS = 86_400_000;

/** How many of the newest days a narrow screen has room for. */
const NARROW_DAYS = 30;

/** `label` is a message key, shown with t(). */
const stateLabels: Record<PublicState, { label: string; text: string; dot: string }> = {
  up: { label: "statusPage.state.up", text: "text-up", dot: "bg-up" },
  down: { label: "statusPage.state.down", text: "text-down", dot: "bg-down" },
  maintenance: { label: "statusPage.state.maintenance", text: "text-maint", dot: "bg-maint" },
  pending: { label: "statusPage.state.pending", text: "text-muted", dot: "bg-idle" },
  paused: { label: "statusPage.state.paused", text: "text-muted", dot: "bg-idle" },
};

/** `title` is a message key, shown with t(). */
const pageStates: Record<StatusPageView["state"], { title: string; tone: string }> = {
  up: { title: "statusPage.overall.up", tone: "border-up/40 bg-up/10 text-up" },
  degraded: { title: "statusPage.overall.degraded", tone: "border-warn/40 bg-warn/12 text-warn" },
  down: { title: "statusPage.overall.down", tone: "border-down/40 bg-down/10 text-down" },
  maintenance: { title: "statusPage.overall.maintenance", tone: "border-maint/40 bg-maint/10 text-maint" },
};

/** The color of a day, by how much of it the monitor was up. */
function dayColor(pct: number | null): string {
  if (pct === null) return "bg-line";
  if (pct >= 99.9) return "bg-up";
  if (pct >= 99) return "bg-up/60";
  if (pct >= 95) return "bg-warn";
  return "bg-down";
}

/** One bar per day, oldest on the left. */
function History({ history, firstDay, t }: { history: (number | null)[]; firstDay: number; t: T }) {
  return (
    <div className="flex h-8 items-stretch gap-[2px]" role="img" aria-label={t.n("statusPage.history.aria", history.length)}>
      {history.map((pct, i) => {
        const date = new Date(firstDay + i * DAY_MS).toLocaleDateString(t.intl, { timeZone: "UTC" });
        return (
          <span
            key={i}
            title={`${date} · ${pct === null ? t("statusPage.noData") : uptime(pct, t)}`}
            className={`min-w-[2px] flex-1 rounded-sm ${dayColor(pct)} ${
              i < history.length - NARROW_DAYS ? "hidden sm:block" : ""
            }`}
          />
        );
      })}
    </div>
  );
}

async function View({ params }: { params: Promise<{ slug: string }> }) {
  // The page is read from the backend each time it is asked for.
  await connection();
  const { slug } = await params;
  const t = await getT();

  let view: StatusPageView;
  try {
    view = await getStatusPageView(slug);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    return (
      <p role="alert" className="card p-6 text-center text-sm text-muted">
        {t("statusPage.unavailable")}
      </p>
    );
  }

  const firstDay = new Date(view.from).getTime();
  const state = pageStates[view.state];
  const daysAgo = (days: number) => t("format.ago", { duration: t.n("format.days", days) });

  return (
    <>
      <header>
        <h1 className="text-3xl font-semibold tracking-tight">{view.title}</h1>
        {view.description && <p className="mt-2 text-muted">{view.description}</p>}
      </header>

      {view.announcement && (
        <section aria-label={t("statusPage.announcement")} className="rounded-xl border border-maint/40 bg-maint/10 p-4">
          <h2 className="text-sm font-semibold text-maint">{t("statusPage.announcement")}</h2>
          <p className="mt-1 whitespace-pre-line text-sm">{view.announcement}</p>
        </section>
      )}

      <p role="status" className={`rounded-xl border p-5 text-lg font-semibold ${state.tone}`}>
        {t(state.title)}
      </p>

      {view.monitors.length === 0 ? (
        <p className="card p-6 text-center text-sm text-muted">{t("statusPage.noMonitors")}</p>
      ) : (
        <ul className="card divide-y divide-line">
          {view.monitors.map((m) => {
            const s = stateLabels[m.state];
            return (
              <li key={m.id} className="p-4">
                <div className="mb-2 flex items-baseline justify-between gap-3">
                  <span className="font-medium">{m.name}</span>
                  <span className={`inline-flex items-center gap-2 whitespace-nowrap text-sm font-medium ${s.text}`}>
                    <span className={`h-2 w-2 rounded-full ${s.dot}`} aria-hidden />
                    {t(s.label)}
                  </span>
                </div>
                <History history={m.history} firstDay={firstDay} t={t} />
                <div className="mt-1.5 flex justify-between text-xs text-muted">
                  <span className="hidden sm:inline">{daysAgo(view.days)}</span>
                  <span className="sm:hidden">{daysAgo(Math.min(view.days, NARROW_DAYS))}</span>
                  <span>
                    {m.uptime_percent === null
                      ? t("statusPage.noData")
                      : t("statusPage.uptime", { value: uptime(m.uptime_percent, t) })}
                  </span>
                  <span>{t("statusPage.today")}</span>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      <section>
        <h2 className="mb-2 text-sm font-semibold">{t("statusPage.incidents.title")}</h2>
        {view.incidents.length === 0 ? (
          <p className="card p-4 text-sm text-muted">{t("statusPage.incidents.none")}</p>
        ) : (
          <ul className="card divide-y divide-line">
            {view.incidents.map((i) => (
              <li key={`${i.monitor_name}-${i.started_at}`} className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 p-4 text-sm">
                <span className="font-medium">{i.monitor_name}</span>
                <span className="text-xs text-muted">
                  <LocalTime iso={i.started_at} seconds={false} />
                  {" · "}
                  {i.resolved_at ? (
                    t("statusPage.incidents.lasted", {
                      duration: span(new Date(i.resolved_at).getTime() - new Date(i.started_at).getTime(), t),
                    })
                  ) : (
                    <span className="font-semibold text-down">{t("statusPage.incidents.ongoing")}</span>
                  )}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
  );
}

export default async function Page({ params }: PageProps<"/status/[slug]">) {
  const t = await getT();
  return (
    <main className="mx-auto flex min-h-full w-full max-w-3xl flex-col gap-6 px-4 py-10">
      <LiveRefresh every="statusPage" />
      <Suspense fallback={<p className="text-sm text-muted">{t("common.loading")}</p>}>
        <View params={params} />
      </Suspense>
      <footer className="mt-auto flex items-center justify-center gap-1.5 pt-6 text-xs text-muted">
        <PulseIcon width={12} height={12} /> {t("statusPage.footer")}
      </footer>
    </main>
  );
}
