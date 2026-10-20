import type { CheckResult } from "@/lib/api";
import { milliseconds } from "@/lib/format";
import type { T } from "@/lib/i18n/translate";
import { ChevronIcon } from "@/app/ui/icons";
import { checkError, WhyBody } from "@/app/ui/monitor-diagnosis";
import { CheckTime } from "@/app/ui/monitor-time";
import { StatusGlyph } from "@/app/ui/status-glyph";

/**
 * Checks one per row, newest first: when, how it went, how long it took and
 * what it said. A failed check opens to the same details as the "Why?"
 * panel of the current failure.
 */
export function CheckRows({ results, inverted = false, t }: { results: CheckResult[]; inverted?: boolean; t: T }) {
  return (
    <div className="mon-checks">
      <div className="mon-check mon-check-head" aria-hidden>
        <span />
        <span>{t("monitorPage.history.time")}</span>
        <span>{t("monitorPage.history.result")}</span>
        <span className="num">{t("monitorPage.history.duration")}</span>
        <span>{t("monitorPage.history.detail")}</span>
      </div>
      <ul>
        {results.map((r) => {
          const row = (
            <>
              {r.up ? <span /> : <ChevronIcon width={14} height={14} />}
              <CheckTime iso={r.checked_at} />
              <span className={`inline-flex items-center gap-2 font-medium ${r.up ? "text-up-ink" : "text-down-ink"}`}>
                <StatusGlyph kind={r.up ? "up" : "down"} />
                {t(r.up ? "monitorPage.check.ok" : "monitorPage.check.failed")}
              </span>
              <span className="num">{milliseconds(r.latency_ms, t)}</span>
              <span className="mon-mono" title={checkError(r) || undefined}>
                {checkError(r)}
              </span>
            </>
          );
          return (
            <li key={r.checked_at}>
              {r.up ? (
                <div className="mon-check">{row}</div>
              ) : (
                <details>
                  <summary className="mon-check">{row}</summary>
                  <div className="mon-check-diag">
                    <WhyBody result={r} inverted={inverted} t={t} />
                  </div>
                </details>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
