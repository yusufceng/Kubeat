import Link from "next/link";
import { ApiError, getWindow, type MonitorWindow } from "@/lib/api";
import { duration } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import { Readouts } from "@/app/ui/readouts";
import { Callout } from "@/app/ui/monitor-callout";
import { CheckRows } from "@/app/ui/monitor-checks";
import { CloseIcon } from "@/app/ui/monitor-icons";
import { IncidentTimeline } from "@/app/ui/monitor-incidents";
import { TimeRange } from "@/app/ui/monitor-time";
import { EventList } from "@/app/ui/pods-panel";

/**
 * What happened to the monitor in the range selected on the chart (or
 * named by a notification's link): the checks and failures Kubeat stored,
 * each failure with why it failed, the incidents that overlap it, and the
 * cluster's events about the pods as far as the cluster still keeps them.
 */
export async function WindowPanel({
  monitorId,
  from,
  to,
  clearHref,
  failuresShown,
  incidentsShown,
  beforeChart,
  hasPods,
  inverted,
}: {
  monitorId: number;
  from: string;
  to: string;
  /** Where "clear the selection" leads. */
  clearHref: string;
  /** How many failed checks to list before pointing at the rest. */
  failuresShown: number;
  /** How many incidents to list before the rest wait behind "Show all". */
  incidentsShown: number;
  /** The selection begins before the chart's span, so no band marks it. */
  beforeChart: boolean;
  /** Whether the page has a Pods section to point at. */
  hasPods: boolean;
  inverted: boolean;
}) {
  const t = await getT();
  let window: MonitorWindow;
  try {
    window = await getWindow(monitorId, from, to);
  } catch (err) {
    return (
      <Callout tone="down">
        {err instanceof ApiError && err.status !== 404 ? err.message : t("monitorPage.window.error")}
      </Callout>
    );
  }
  const span = Math.round((Date.parse(window.to) - Date.parse(window.from)) / 1000);
  const warnings = window.events.filter((e) => e.type === "Warning").length;
  const checks = window.checks;
  const answered = checks && checks.total > checks.failed;

  return (
    <section id="selected-range" className="panel scroll-mt-4" aria-labelledby="mon-window-title">
      <div className="mon-ph">
        <h2 id="mon-window-title">{t("monitorPage.window.title")}</h2>
        <span className="mon-sub">
          <TimeRange from={window.from} to={window.to} /> · {duration(span, t)}
        </span>
        <span className="mon-ph-end">
          <Link href={clearHref} scroll={false} className="btn btn-ghost btn-sm">
            <CloseIcon width={14} height={14} />
            {t("monitorPage.window.clear")}
          </Link>
        </span>
      </div>
      {beforeChart && <p className="mon-help px-4 text-warn-ink!">{t("monitorPage.window.beforeChart")}</p>}

      <div className={checks ? "pt-3" : "px-4 pt-3"}>
        {checks ? (
          <Readouts
            label={t("monitorPage.window.figures")}
            // A band across the panel rather than a box inside it.
            className="rounded-none border-x-0 bg-transparent"
            items={[
              { label: t("monitorPage.window.checks"), value: checks.total.toLocaleString(t.intl) },
              {
                label: t("monitorPage.window.failed"),
                glyph: checks.failed > 0 ? "down" : undefined,
                value: checks.failed.toLocaleString(t.intl),
                tone: checks.failed > 0 ? "down" : "muted",
              },
              {
                label: t("monitorPage.window.avg"),
                value: answered ? Math.round(checks.avg_latency_ms).toLocaleString(t.intl) : t("common.none"),
                unit: answered ? "ms" : undefined,
                tone: answered ? undefined : "muted",
              },
              {
                label: t("monitorPage.window.max"),
                value: answered ? Math.round(checks.max_latency_ms).toLocaleString(t.intl) : t("common.none"),
                unit: answered ? "ms" : undefined,
                tone: answered ? undefined : "muted",
              },
            ]}
          />
        ) : (
          <p className="text-[13px] text-ink-3">{t("monitorPage.window.noRaw")}</p>
        )}
        {checks?.partial && <p className="mon-help px-4">{t("monitorPage.window.partial")}</p>}
      </div>

      <div className={`mon-window-cols ${checks && !checks.partial ? "" : "mt-4 border-t border-line"}`}>
        <div className="min-w-0 pt-3">
          <h3 className="mon-h3 px-4">{t("monitorPage.incidents.title")}</h3>
          {window.incidents.length === 0 ? (
            <p className="px-4 pb-4 text-[13px] text-ink-3">{t("monitorPage.window.incidents.none")}</p>
          ) : (
            <IncidentTimeline incidents={window.incidents} shown={incidentsShown} t={t} />
          )}
        </div>
        <div className="min-w-0 px-4 pt-3 pb-4">
          <h3 className="mon-h3">
            {t("monitorPage.window.events")}
            {window.events.length > 0 && (
              <span className="ml-2 font-normal">
                {t.n("monitorPage.window.events.count", window.events.length)}
                {warnings > 0 && `, ${t.n("monitorPage.window.events.warnings", warnings)}`}
              </span>
            )}
          </h3>
          {window.events.length === 0 ? (
            <p className="text-[13px] text-ink-3">
              {window.events_reason
                ? t("monitorPage.window.events.unavailable", { reason: window.events_reason })
                : t("monitorPage.window.events.none")}
            </p>
          ) : (
            <EventList events={window.events} />
          )}
          {window.events_truncated && (
            <p className="mon-help">{t("monitorPage.window.events.truncated", { count: window.events.length })}</p>
          )}
        </div>
      </div>

      {window.failures.length > 0 && (
        <div className="border-t border-line pt-3">
          <h3 className="mon-h3 px-4">{t("monitorPage.window.failures")}</h3>
          <CheckRows results={window.failures.slice(0, failuresShown)} inverted={inverted} t={t} />
          {(window.failures.length > failuresShown || window.failures_truncated) && (
            <p className="mon-help border-t border-line px-4 pt-2 pb-3">
              {t("monitorPage.window.failures.more", {
                shown: Math.min(failuresShown, window.failures.length),
                total: window.checks?.failed ?? window.failures.length,
              })}
            </p>
          )}
        </div>
      )}

      {hasPods && <p className="mon-help border-t border-line px-4 pt-2.5 pb-3">{t("monitorPage.window.podsNote")}</p>}
    </section>
  );
}
