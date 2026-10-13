import Link from "next/link";
import { ApiError, getWindow } from "@/lib/api";
import { duration, milliseconds } from "@/lib/format";
import { getT } from "@/lib/i18n/server";
import { IncidentTable } from "@/app/ui/incident-table";
import { LocalTime } from "@/app/ui/local-time";
import { StatusBadge } from "@/app/ui/status";

/**
 * What happened to the monitor in the range selected on the chart (or
 * named by a notification's link): the checks and failures Kubeat stored,
 * the incidents that overlap it, and the cluster's events about the pods
 * as far as the cluster still keeps them.
 */
export async function WindowPanel({
  monitorId,
  from,
  to,
  clearHref,
  failuresShown,
  beforeChart,
  hasPods,
}: {
  monitorId: number;
  from: string;
  to: string;
  /** Where "clear the selection" leads. */
  clearHref: string;
  /** How many failed checks to list before pointing at the rest. */
  failuresShown: number;
  /** The selection begins before the chart's span, so no band marks it. */
  beforeChart: boolean;
  /** Whether the page has a Pods section to point at. */
  hasPods: boolean;
}) {
  const t = await getT();
  let window;
  try {
    window = await getWindow(monitorId, from, to);
  } catch (err) {
    return (
      <section className="card border-down/40 p-5 text-sm text-down" aria-label={t("monitorPage.window.title")}>
        {err instanceof ApiError && err.status !== 404 ? err.message : t("monitorPage.window.error")}
      </section>
    );
  }
  const span = Math.round((new Date(window.to).getTime() - new Date(window.from).getTime()) / 1000);
  const warnings = window.events.filter((e) => e.type === "Warning").length;

  return (
    <section id="selected-range" className="card scroll-mt-4 p-5" aria-label={t("monitorPage.window.title")}>
      <div className="flex flex-wrap items-baseline justify-between gap-3">
        <h2 className="text-sm font-semibold">
          {t("monitorPage.window.title")}{" "}
          <span className="font-normal text-muted">
            <LocalTime iso={window.from} /> – <LocalTime iso={window.to} /> · {duration(span, t)}
          </span>
        </h2>
        <Link href={clearHref} scroll={false} className="text-xs text-muted hover:text-ink">
          {t("monitorPage.window.clear")}
        </Link>
      </div>
      {beforeChart && <p className="mt-2 text-xs text-warn">{t("monitorPage.window.beforeChart")}</p>}

      <dl className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {window.checks ? (
          <>
            <div className="rounded-lg bg-sunken px-3 py-2">
              <dt className="text-xs text-muted">{t("monitorPage.window.checks")}</dt>
              <dd className="mt-0.5 text-lg font-semibold tabular-nums">{window.checks.total}</dd>
            </div>
            <div className="rounded-lg bg-sunken px-3 py-2">
              <dt className="text-xs text-muted">{t("monitorPage.window.failed")}</dt>
              <dd className={`mt-0.5 text-lg font-semibold tabular-nums ${window.checks.failed > 0 ? "text-down" : ""}`}>
                {window.checks.failed}
              </dd>
            </div>
            <div className="rounded-lg bg-sunken px-3 py-2">
              <dt className="text-xs text-muted">{t("monitorPage.window.avg")}</dt>
              <dd className="mt-0.5 text-lg font-semibold tabular-nums">
                {window.checks.total > window.checks.failed ? milliseconds(window.checks.avg_latency_ms, t) : "–"}
              </dd>
            </div>
            <div className="rounded-lg bg-sunken px-3 py-2">
              <dt className="text-xs text-muted">{t("monitorPage.window.max")}</dt>
              <dd className="mt-0.5 text-lg font-semibold tabular-nums">
                {window.checks.total > window.checks.failed ? milliseconds(window.checks.max_latency_ms, t) : "–"}
              </dd>
            </div>
          </>
        ) : (
          <p className="col-span-full text-sm text-muted">{t("monitorPage.window.noRaw")}</p>
        )}
        {window.checks?.partial && <p className="col-span-full text-xs text-muted">{t("monitorPage.window.partial")}</p>}
      </dl>

      <div className="mt-5 flex flex-col gap-5">
        <div>
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{t("nav.incidents")}</h3>
          {window.incidents.length === 0 ? (
            <p className="text-sm text-muted">{t("monitorPage.window.incidents.none")}</p>
          ) : (
            <IncidentTable incidents={window.incidents} showMonitor={false} />
          )}
        </div>

        <div>
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">
            {t("monitorPage.window.events")}
            {window.events.length > 0 && (
              <span className="ml-2 font-normal normal-case tracking-normal">
                {t.n("monitorPage.window.events.count", window.events.length)}
                {warnings > 0 && `, ${t.n("monitorPage.window.events.warnings", warnings)}`}
              </span>
            )}
          </h3>
          {window.events.length === 0 ? (
            <p className="text-sm text-muted">
              {window.events_reason
                ? t("monitorPage.window.events.unavailable", { reason: window.events_reason })
                : t("monitorPage.window.events.none")}
            </p>
          ) : (
            <ol className="divide-y divide-line rounded-lg border border-line text-xs">
              {window.events.map((e, i) => (
                <li key={`${e.at}-${e.object}-${e.reason}-${i}`} className="flex flex-wrap gap-x-3 gap-y-0.5 px-3 py-1.5">
                  <span className="tabular-nums text-muted">
                    <LocalTime iso={e.at} />
                  </span>
                  <span className={`font-medium ${e.type === "Warning" ? "text-warn" : ""}`}>
                    {e.reason}
                    {e.count > 1 && <span className="font-normal text-muted"> ×{e.count}</span>}
                  </span>
                  <span className="font-mono text-muted">{e.object}</span>
                  <span className="basis-full text-muted">{e.message}</span>
                </li>
              ))}
            </ol>
          )}
          {window.events_truncated && (
            <p className="mt-2 text-xs text-muted">{t("monitorPage.window.events.truncated", { count: window.events.length })}</p>
          )}
        </div>
      </div>

      {window.failures.length > 0 && (
        <div className="mt-5">
          <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">
            {t("monitorPage.window.failures")}
          </h3>
          <div className="overflow-x-auto rounded-lg border border-line">
            <table className="w-full text-left text-xs">
              <thead className="border-b border-line text-muted">
                <tr>
                  <th className="px-3 py-1.5 font-medium">{t("monitorPage.history.time")}</th>
                  <th className="px-3 py-1.5 font-medium">{t("monitorPage.history.result")}</th>
                  <th className="px-3 py-1.5 text-right font-medium">{t("monitorPage.history.duration")}</th>
                  <th className="px-3 py-1.5 font-medium">{t("monitorPage.history.detail")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {window.failures.slice(0, failuresShown).map((r) => (
                  <tr key={r.checked_at}>
                    <td className="whitespace-nowrap px-3 py-1.5 tabular-nums">
                      <LocalTime iso={r.checked_at} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5">
                      <StatusBadge state="down" t={t} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 text-right tabular-nums">{r.latency_ms} ms</td>
                    <td className="px-3 py-1.5 font-mono text-muted">
                      {[r.status_code && `HTTP ${r.status_code}`, r.error ?? r.detail].filter(Boolean).join(" · ")}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {(window.failures.length > failuresShown || window.failures_truncated) && (
            <p className="mt-2 text-xs text-muted">
              {t("monitorPage.window.failures.more", {
                shown: Math.min(failuresShown, window.failures.length),
                total: window.checks?.failed ?? window.failures.length,
              })}
            </p>
          )}
        </div>
      )}

      {hasPods && <p className="mt-5 text-xs text-muted">{t("monitorPage.window.podsNote")}</p>}
    </section>
  );
}
