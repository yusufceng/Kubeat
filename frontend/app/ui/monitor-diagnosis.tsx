import type { CheckResult, Diagnostics, DiagnosticTiming, DiagnosticTls } from "@/lib/api";
import type { T } from "@/lib/i18n/translate";
import { stepLength, waterfall } from "@/lib/monitor-page";
import { CalendarDate } from "@/app/ui/monitor-time";
import { StatusGlyph } from "@/app/ui/status-glyph";
import { TimeAgo } from "@/app/ui/time-ago";

/**
 * What a failure most likely means, in the viewer's language: the code
 * worded with its values, or the API's English for a code this version of
 * the interface does not know yet; null when nothing recognised it.
 */
export function explanationText(d: Diagnostics, t: T): string | null {
  const e = d.explanation;
  if (!e) return null;
  const key = `monitorPage.why.code.${e.code}`;
  const text = t(key, e.params);
  // The translator answers with the key itself when it has no wording.
  return text !== key ? text : e.text || null;
}

/** The error a check reported, with its status code when the error does not say it. */
export function checkError(r: Pick<CheckResult, "status_code" | "error" | "detail">): string {
  const code = r.status_code ? `HTTP ${r.status_code}` : null;
  const error = r.error ?? r.detail;
  if (!error) return code ?? "";
  return code && !error.includes(String(r.status_code)) ? `${code} · ${error}` : error;
}

/**
 * Why a check failed, as far as Kubeat could tell: what it most likely
 * means and the error, the address it reached, the answer, the pod; then
 * where its time went, the certificate the server showed and the few
 * headers that are kept. The same for the current failure and for any
 * failed check of the history.
 */
export function WhyBody({ result, inverted = false, t }: { result: CheckResult; inverted?: boolean; t: T }) {
  const d = result.diagnostics ?? null;
  const explanation = d ? explanationText(d, t) : null;
  const error = checkError(result);
  const headers = d ? (Object.entries(d.headers) as [string, string][]) : [];
  const side = Boolean(d?.timing || d?.tls || headers.length > 0);

  return (
    <div className={`mon-why-body ${side ? "mon-why-2" : ""}`}>
      <div className="min-w-0">
        {explanation ? (
          <p className="mon-why-explain">{explanation}</p>
        ) : d ? (
          <p className="mon-why-note">{t(inverted ? "monitorPage.why.inverted" : "monitorPage.why.unknown")}</p>
        ) : null}
        {error && (
          <p className={explanation || d ? "mon-why-error" : "mon-why-error mt-0"}>
            <span className="sr-only">{t("monitorPage.why.error")}: </span>
            {error}
          </p>
        )}
        {d && (d.remote_addr || d.status_line || d.redirects > 0 || d.pod) && (
          <dl className="mon-facts">
            {d.remote_addr && (
              <div className="mon-fact">
                <dt>{t("monitorPage.why.address")}</dt>
                <dd className="mon-mono">{d.remote_addr}</dd>
              </div>
            )}
            {(d.status_line || d.redirects > 0) && (
              <div className="mon-fact">
                <dt>{t(d.status_line ? "monitorPage.why.answer" : "monitorPage.why.redirects")}</dt>
                <dd>
                  {d.status_line && <span className="mon-mono">{d.status_line}</span>}
                  {d.redirects > 0 && (
                    <small className={d.status_line ? undefined : "ml-0!"}>
                      {t.n("monitorPage.why.redirects.count", d.redirects)}
                    </small>
                  )}
                </dd>
              </div>
            )}
            {d.pod && (
              <div className="mon-fact">
                <dt>{t("monitorPage.why.pod")}</dt>
                <dd>
                  <span className="mon-mono">{d.pod}</span>
                  <small>{t("monitorPage.why.podHint")}</small>
                </dd>
              </div>
            )}
          </dl>
        )}
      </div>
      {side && d && (
        <div className="flex min-w-0 flex-col gap-5">
          {d.timing && <Waterfall timing={d.timing} t={t} />}
          {d.tls && <Certificate tls={d.tls} checkedAt={result.checked_at} t={t} />}
          {headers.length > 0 && (
            <section>
              <h3 className="mon-h3">{t("monitorPage.why.headers")}</h3>
              <dl>
                {headers.map(([name, value]) => (
                  <div key={name} className="mon-fact">
                    <dt className="mon-mono">{name}</dt>
                    <dd className="mon-mono">{value}</dd>
                  </div>
                ))}
              </dl>
            </section>
          )}
        </div>
      )}
    </div>
  );
}

/** A step's length with its unit set small. */
function Length({ ms, t }: { ms: number; t: T }) {
  const { value, unit } = stepLength(ms, t.intl);
  return (
    <>
      {value}
      <small>{unit === "s" ? t("monitorPage.why.seconds") : "ms"}</small>
    </>
  );
}

/**
 * Where the time of the check went: the lookup, the connection, the
 * handshake and the wait for the first byte, one after another on an axis
 * that ends with the check. A step that did not happen says so; a check
 * that got no answer ends in a hatched red stretch of waiting.
 */
function Waterfall({ timing, t }: { timing: DiagnosticTiming; t: T }) {
  const { phases, total, unanswered } = waterfall(timing);
  const at = (ms: number) => `${total > 0 ? (ms / total) * 100 : 0}%`;
  const span = (from: number, to: number) => ({ left: at(from), width: at(to - from) });
  const skipped = phases.some((p) => p.state === "skipped");
  // The wait for an answer that never came is drawn when it is long enough
  // to matter: a handshake that failed outright leaves only a sliver.
  const waited = unanswered && unanswered.to - unanswered.from >= Math.max(1, total * 0.03) ? unanswered : null;

  return (
    <section>
      <h3 className="mon-h3">{t("monitorPage.why.timing")}</h3>
      <dl className="mon-wf">
        {phases.map((p) =>
          p.key === "firstByte" && unanswered ? (
            <Row
              key={p.key}
              label={<span className="text-down-ink">{t("monitorPage.why.phase.unanswered")}</span>}
              bar={waited && <span className="mon-wf-bar mon-wf-fail" style={span(waited.from, waited.to)} />}
              value={waited ? <Length ms={waited.to - waited.from} t={t} /> : <span className="mon-wf-skip">–</span>}
            />
          ) : (
            <Row
              key={p.key}
              label={t(`monitorPage.why.phase.${p.key}`)}
              bar={p.state === "done" && <span className="mon-wf-bar" style={span(p.from, p.to)} />}
              value={
                p.state === "done" ? (
                  <Length ms={p.to - p.from} t={t} />
                ) : (
                  <span className="mon-wf-skip">{p.state === "skipped" ? t("monitorPage.why.phase.skipped") : "–"}</span>
                )
              }
            />
          ),
        )}
        <span className="mon-wf-sep" aria-hidden />
        <Row
          label={t("monitorPage.why.phase.total")}
          bar={<span className="mon-wf-bar mon-wf-total" style={{ left: 0, width: "100%" }} />}
          value={<Length ms={total} t={t} />}
        />
      </dl>
      {skipped && <p className="mon-help">{t("monitorPage.why.skippedNote")}</p>}
    </section>
  );
}

function Row({ label, bar, value }: { label: React.ReactNode; bar: React.ReactNode; value: React.ReactNode }) {
  return (
    <>
      <dt>{label}</dt>
      <dd aria-hidden className="mon-wf-track">
        {bar}
      </dd>
      <dd>{value}</dd>
    </>
  );
}

/**
 * The certificate the server presented, also one that failed verification:
 * its names, who issued it and until when it holds, counted from the check
 * itself so that the page needs no clock of its own.
 */
function Certificate({ tls, checkedAt, t }: { tls: DiagnosticTls; checkedAt: string; t: T }) {
  const days = tls.not_after ? Math.floor((Date.parse(tls.not_after) - Date.parse(checkedAt)) / 86_400_000) : null;
  return (
    <section>
      <h3 className="mon-h3">{t("monitorPage.why.tls")}</h3>
      <dl>
        {tls.version && (
          <div className="mon-fact">
            <dt>{t("monitorPage.why.tls.version")}</dt>
            <dd>{tls.version}</dd>
          </div>
        )}
        {tls.subject && (
          <div className="mon-fact">
            <dt>{t("monitorPage.why.tls.subject")}</dt>
            <dd className="mon-mono">{tls.subject}</dd>
          </div>
        )}
        {tls.issuer && (
          <div className="mon-fact">
            <dt>{t("monitorPage.why.tls.issuer")}</dt>
            <dd className="mon-mono">{tls.issuer}</dd>
          </div>
        )}
        {tls.sans.length > 0 && (
          <div className="mon-fact">
            <dt>{t("monitorPage.why.tls.names")}</dt>
            <dd>
              <span className="mon-mono">{tls.sans.join(", ")}</span>
              {tls.more_sans > 0 && <small>{t.n("monitorPage.why.tls.moreNames", tls.more_sans)}</small>}
            </dd>
          </div>
        )}
        {tls.not_after && (
          <div className="mon-fact">
            <dt>{t("monitorPage.why.tls.notAfter")}</dt>
            <dd>
              <CalendarDate iso={tls.not_after} />
              {days !== null && (
                <small className={days < 0 ? "text-down-ink!" : undefined}>
                  {days < 0 ? t("monitorPage.why.tls.expired") : t.n("monitorPage.why.tls.daysLeft", days)}
                </small>
              )}
            </dd>
          </div>
        )}
      </dl>
    </section>
  );
}

/**
 * "Why?": the current failure explained, high on the page while the
 * monitor fails, with when the failed check ran.
 */
export function WhyPanel({ result, inverted, t }: { result: CheckResult; inverted: boolean; t: T }) {
  return (
    <section className="panel" aria-labelledby="mon-why-title">
      <div className="mon-why-head">
        <StatusGlyph kind="down" />
        <h2 id="mon-why-title">{t("monitorPage.why.title")}</h2>
        <span className="mon-sub">
          {t("monitorPage.why.when")} · <TimeAgo iso={result.checked_at} />
        </span>
      </div>
      <WhyBody result={result} inverted={inverted} t={t} />
    </section>
  );
}
