"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { saveMonitorAction } from "@/app/actions";
import type { DnsRecordType, HttpMethod, JsonOperator, Monitor, MonitorType } from "@/lib/api";
import { MASKED_VALUE } from "@/lib/constants";
import { duration } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { typeWords } from "@/lib/monitor-page";
import { isDatabaseType, monitorTypes } from "@/lib/monitor-types";
import { externalTarget } from "@/lib/target-class";

const httpMethods: HttpMethod[] = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"];
// The labels are message keys; shown with t().
const jsonOperators: { value: JsonOperator; label: string }[] = [
  { value: "==", label: "monitorForm.jsonOperator.eq" },
  { value: "!=", label: "monitorForm.jsonOperator.ne" },
  { value: ">", label: "monitorForm.jsonOperator.gt" },
  { value: ">=", label: "monitorForm.jsonOperator.ge" },
  { value: "<", label: "monitorForm.jsonOperator.lt" },
  { value: "<=", label: "monitorForm.jsonOperator.le" },
  { value: "contains", label: "monitorForm.jsonOperator.contains" },
];
const dnsRecordTypes: DnsRecordType[] = ["A", "AAAA", "CNAME", "MX", "NS", "TXT", "SRV", "PTR"];
const contentTypes = [
  { value: "application/json", label: "monitorForm.contentType.json" },
  { value: "application/x-www-form-urlencoded", label: "monitorForm.contentType.form" },
  { value: "application/xml", label: "monitorForm.contentType.xml" },
  { value: "text/plain", label: "monitorForm.contentType.text" },
];

/**
 * The kinds of check as the picker offers them: what lives in the cluster
 * first (a workload, then what reaches Services), the databases, and last
 * the checks that need no cluster at all.
 */
const typeOrder: MonitorType[] = [
  "workload",
  "http",
  "tcp",
  "grpc",
  "dns",
  "websocket",
  "postgres",
  "mysql",
  "redis",
  "ping",
  "push",
];

export type MonitorDefaults = { interval: number; timeout: number; failureThreshold: number; slowAboveMs: number };

/** A whole number of 1 or more typed into a field, or null. */
function whole(value: string, min = 1): number | null {
  const n = Number(value);
  return value.trim() !== "" && Number.isInteger(n) && n >= min ? n : null;
}

/** A message with its {values} set in bold: the help that reads the form back. */
function echo(text: string, values: Record<string, React.ReactNode>): React.ReactNode[] {
  return text.split(/(\{\w+\})/).map((part, i) => {
    const name = /^\{(\w+)\}$/.exec(part)?.[1];
    return name && name in values ? <b key={i}>{values[name]}</b> : part;
  });
}

/**
 * A part of the form: its title and what it is for on the left, its fields
 * on the right, where the form is wide enough; one above the other where
 * it is not.
 */
function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="mon-fsec" aria-label={title}>
      <div className="mon-fsec-intro">
        <h2>{title}</h2>
        {hint && <p>{hint}</p>}
      </div>
      <div className="mon-fields">
        <div className="mon-fields-grid">{children}</div>
      </div>
    </section>
  );
}

/** Columns of six a field takes where the fields have room. */
type Span = 2 | 3 | 4 | 6;
const spans: Record<Span, string> = { 2: "mon-span-2", 3: "mon-span-3", 4: "mon-span-4", 6: "" };

function Field({
  label,
  optional,
  hint,
  span = 3,
  children,
}: {
  label: string;
  /** The words that mark the field optional, set small after the label. */
  optional?: string;
  hint?: React.ReactNode;
  span?: Span;
  children: React.ReactNode;
}) {
  return (
    <label className={`block ${spans[span]}`}>
      <span className="mon-label">
        {label}
        {optional && <small>{optional}</small>}
      </span>
      {children}
      {hint && <span className="mon-help">{hint}</span>}
    </label>
  );
}

/** A number of seconds, with its unit set inside the field. */
function Seconds({
  unit,
  ...props
}: React.InputHTMLAttributes<HTMLInputElement> & { unit: string }) {
  return (
    <span className="relative block">
      <input type="number" inputMode="numeric" {...props} className="field pr-10 tabular-nums" />
      <span aria-hidden className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-ink-3">
        {unit}
      </span>
    </span>
  );
}

function Check({
  name,
  label,
  hint,
  defaultChecked,
  disabled,
}: {
  name: string;
  label: string;
  hint?: string;
  defaultChecked?: boolean;
  disabled?: boolean;
}) {
  return (
    <label className="mon-check-field">
      <input type="checkbox" name={name} defaultChecked={defaultChecked} disabled={disabled} />
      <span>
        <b>{label}</b>
        {hint && <span className="mon-help mt-0.5">{hint}</span>}
      </span>
    </label>
  );
}

/** A small set of choices as a segmented control: radios, so the form posts the one picked. */
function Segmented<V extends string>({
  name,
  label,
  options,
  value,
  defaultValue,
  onChange,
}: {
  name: string;
  label: string;
  options: { value: V; label: string }[];
  value?: V;
  defaultValue?: V;
  onChange?: (value: V) => void;
}) {
  return (
    <div role="radiogroup" aria-label={label} className="mon-seg flex w-full">
      {options.map((o) => (
        <label key={o.value} className="flex-1">
          <input
            type="radio"
            name={name}
            value={o.value}
            {...(value !== undefined ? { checked: value === o.value } : { defaultChecked: defaultValue === o.value })}
            onChange={onChange ? () => onChange(o.value) : undefined}
          />
          {o.label}
        </label>
      ))}
    </div>
  );
}

/** The timing of the check read back as sentences, from what is typed now. */
function TimingEcho({
  push,
  interval,
  timeout,
  threshold,
  retry,
  t,
}: {
  push: boolean;
  interval: string;
  timeout: string;
  threshold: string;
  retry: string;
  t: T;
}) {
  const i = whole(interval);
  const o = whole(timeout);
  const n = whole(threshold);
  const r = whole(retry, 0);
  if (i === null || o === null || n === null || (!push && r === null)) {
    return <p className="mon-echo">{t("monitorForm.echo.invalid")}</p>;
  }
  const d = (s: number) => duration(s, t);
  // The plural form as it is written, its {count} still in place for echo().
  const form = (key: string, count: number) => t(`${key}.${count === 1 ? "one" : "other"}`);
  return (
    <p className="mon-echo" aria-live="polite">
      {push ? (
        <>
          {echo(t("monitorForm.echo.push"), { interval: d(i), grace: d(o) })}{" "}
          {echo(form("monitorForm.echo.pushThreshold", n), { count: n })}
        </>
      ) : (
        <>
          {echo(t("monitorForm.echo.check"), { interval: d(i), timeout: d(o) })}{" "}
          {echo(form("monitorForm.echo.threshold", n), { count: n })}{" "}
          {n > 1 && (r! > 0 ? echo(t("monitorForm.echo.retry"), { retry: d(r!) }) : t("monitorForm.echo.noRetry"))}
        </>
      )}
    </p>
  );
}

/**
 * The form of a monitor: every setting of every type. With a monitor it
 * edits that one; without, it creates one of the type picked at the top.
 * Each part has its fields on the right and says what it is for on the
 * left; the timing reads its values back as sentences, and Save stays in
 * reach at the bottom of the window.
 */
export function MonitorForm({
  monitor,
  defaults,
  groups,
  externalMinInterval = 0,
}: {
  monitor?: Monitor;
  defaults: MonitorDefaults;
  /** The groups that exist, offered as suggestions. */
  groups: string[];
  /** The shortest interval, in seconds, of a target outside the cluster; 0 when there is no separate rule. */
  externalMinInterval?: number;
}) {
  const t = useT();
  const [type, setType] = useState<MonitorType>(monitor?.type ?? "http");
  const [target, setTarget] = useState(monitor?.target ?? "");
  const [auth, setAuth] = useState(monitor?.http?.auth ?? "");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  // The values the help reads back.
  const [every, setEvery] = useState(String(monitor?.interval_seconds ?? defaults.interval));
  const [within, setWithin] = useState(String(monitor?.timeout_seconds ?? defaults.timeout));
  const [threshold, setThreshold] = useState(String(monitor?.failure_threshold ?? defaults.failureThreshold));
  const [retry, setRetry] = useState(String(monitor?.retry_interval_seconds ?? 0));
  const [slowAbove, setSlowAbove] = useState(String(monitor?.slow_above_ms ?? defaults.slowAboveMs));
  const [redirects, setRedirects] = useState(String(monitor?.http?.max_redirects ?? 10));
  const [minReady, setMinReady] = useState(String(monitor?.workload?.min_ready ?? 0));

  const info = monitorTypes[type];
  const http = monitor?.http;
  const push = type === "push";
  // A discovered monitor is named after the object it was made from. One
  // made from an annotation also takes its target and timing from there.
  const discovered = (monitor?.source ?? "") !== "";
  const managed = monitor?.managed ?? false;
  // Only a service port has pods to check one by one.
  const fromService = (monitor?.source ?? "").startsWith("service/") && (type === "http" || type === "tcp");
  const optional = t("common.optional");
  const seconds = t("monitorForm.unit.seconds");
  // Said beforehand, as the server would refuse it: an external target has
  // a longer shortest interval. The same rule as the server's, on the name
  // as written.
  const externalHint =
    externalMinInterval > 0 && externalTarget(type, target)
      ? t("monitorForm.timing.interval.externalHint", { seconds: externalMinInterval })
      : undefined;

  // Submitted by hand instead of through the form's action: React resets a
  // form after its action, which would throw away a rejected form's values.
  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    setError(null);
    startTransition(async () => {
      const result = await saveMonitorAction(data);
      if (result?.error) setError(result.error); // on success the action redirects
    });
  }

  const redirectCount = whole(redirects, 0);
  const minReadyCount = whole(minReady, 0);
  // The slow threshold read back: off, over the timeout (which the backend
  // refuses), or what crossing it does.
  const slowMs = whole(slowAbove, 0);
  const timeoutSeconds = whole(within);
  const slowHint =
    slowMs === null ? (
      t("monitorForm.timing.slowAbove.invalid")
    ) : slowMs === 0 ? (
      t("monitorForm.timing.slowAbove.off")
    ) : timeoutSeconds !== null && slowMs >= timeoutSeconds * 1000 ? (
      <span className="text-down-ink">
        {t("monitorForm.timing.slowAbove.aboveTimeout", { timeout: duration(timeoutSeconds, t) })}
      </span>
    ) : (
      t.n("monitorForm.timing.slowAbove.hint", whole(threshold) ?? 1, { ms: slowMs.toLocaleString(t.intl) })
    );

  return (
    <form onSubmit={submit} className="panel mon-form">
      {monitor && <input type="hidden" name="id" value={monitor.id} />}
      {monitor && <input type="hidden" name="type" value={type} />}

      <Section title={t("monitorForm.general.title")} hint={t("monitorForm.general.hint")}>
        {monitor ? (
          <Field label={t("monitorForm.general.type")} span={2}>
            <input defaultValue={t(info.label)} disabled className="field" />
          </Field>
        ) : (
          <fieldset className="min-w-0">
            <legend className="mon-label">{t("monitorForm.general.type")}</legend>
            <div className="mon-types">
              {typeOrder.map((mt) => (
                <label key={mt} className="mon-type">
                  <input
                    type="radio"
                    name="type"
                    value={mt}
                    checked={type === mt}
                    onChange={() => setType(mt)}
                  />
                  <span className="mon-type-word">{typeWords[mt]}</span>
                  <span className="mon-type-name">{t(monitorTypes[mt].label)}</span>
                </label>
              ))}
            </div>
            <p className="mon-help" aria-live="polite">
              {echo(`{type}: ${t(info.summary)}`, { type: t(info.label) })}
            </p>
          </fieldset>
        )}
        {discovered ? (
          <Field label={t("monitorForm.general.name")} span={monitor ? 4 : 6} hint={t("monitorForm.general.name.discoveredHint")}>
            <input defaultValue={monitor?.name} disabled className="field" />
          </Field>
        ) : (
          <Field label={t("monitorForm.general.name")} span={monitor ? 4 : 6}>
            <input
              name="name"
              required
              defaultValue={monitor?.name}
              placeholder={t("monitorForm.general.name.placeholder")}
              className="field"
            />
          </Field>
        )}
        {info.target ? (
          <Field label={t(info.target.label)} span={6}>
            <input
              name="target"
              required
              readOnly={managed}
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder={t(info.target.placeholder)}
              className="field font-mono"
            />
          </Field>
        ) : (
          <p className="mon-echo">{t("monitorForm.general.pushNote")}</p>
        )}
        <Field label={t("monitorForm.general.description")} optional={optional} span={6}>
          <input
            name="description"
            defaultValue={monitor?.description}
            maxLength={500}
            placeholder={t("monitorForm.general.description.placeholder")}
            className="field"
          />
        </Field>
        {discovered ? (
          <Field label={t("monitorForm.general.namespace")} span={3} hint={t("monitorForm.general.namespace.discoveredHint")}>
            <input defaultValue={monitor?.namespace} disabled className="field font-mono" />
          </Field>
        ) : (
          <Field label={t("monitorForm.general.group")} optional={optional} span={3} hint={t("monitorForm.general.group.hint")}>
            <input
              name="group"
              list="monitor-groups"
              defaultValue={monitor?.namespace}
              maxLength={63}
              placeholder={t("monitorForm.general.group.placeholder")}
              className="field"
            />
            <datalist id="monitor-groups">
              {groups.map((g) => (
                <option key={g} value={g} />
              ))}
            </datalist>
          </Field>
        )}
        <Field label={t("monitorForm.general.tags")} optional={optional} span={3} hint={t("monitorForm.general.tags.hint")}>
          <input
            name="tags"
            defaultValue={monitor?.tags.join(", ")}
            placeholder={t("monitorForm.general.tags.placeholder")}
            className="field"
          />
        </Field>
      </Section>

      <Section
        title={t("monitorForm.timing.title")}
        hint={managed ? t("monitorForm.timing.managedHint") : push ? t("monitorForm.timing.pushHint") : t("monitorForm.timing.hint")}
      >
        <Field label={push ? t("monitorForm.timing.reportInterval") : t("monitorForm.timing.interval")} span={push ? 2 : 3} hint={externalHint}>
          <Seconds
            name="interval_seconds"
            min={1}
            required
            readOnly={managed}
            value={every}
            onChange={(e) => setEvery(e.target.value)}
            unit={seconds}
          />
        </Field>
        <Field label={push ? t("monitorForm.timing.gracePeriod") : t("monitorForm.timing.timeout")} span={push ? 2 : 3}>
          <Seconds
            name="timeout_seconds"
            min={1}
            required
            readOnly={managed}
            value={within}
            onChange={(e) => setWithin(e.target.value)}
            unit={seconds}
          />
        </Field>
        <Field label={t("monitorForm.timing.threshold")} span={push ? 2 : 3} hint={t("monitorForm.timing.threshold.hint")}>
          <span className="relative block">
            <input
              name="failure_threshold"
              type="number"
              inputMode="numeric"
              min={1}
              required
              readOnly={managed}
              value={threshold}
              onChange={(e) => setThreshold(e.target.value)}
              className="field pr-16 tabular-nums"
            />
            <span aria-hidden className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs text-ink-3">
              {t("monitorForm.unit.checks")}
            </span>
          </span>
        </Field>
        {!push && (
          <Field label={t("monitorForm.timing.retryInterval")} span={3} hint={t("monitorForm.timing.retryInterval.hint")}>
            <Seconds
              name="retry_interval_seconds"
              min={0}
              value={retry}
              onChange={(e) => setRetry(e.target.value)}
              unit={seconds}
            />
          </Field>
        )}
        {/* Not a managed setting: editable on an annotated monitor too. */}
        <Field label={t("monitorForm.timing.slowAbove")} optional={optional} span={3} hint={slowHint}>
          <Seconds name="slow_above_ms" min={0} value={slowAbove} onChange={(e) => setSlowAbove(e.target.value)} unit="ms" />
        </Field>
        <TimingEcho push={push} interval={every} timeout={within} threshold={threshold} retry={retry} t={t} />
      </Section>

      {type === "http" && (
        <>
          <Section title={t("monitorForm.request.title")} hint={t("monitorForm.request.hint")}>
            <Field label={t("monitorForm.request.method")} span={2}>
              <select name="http_method" defaultValue={http?.method ?? "GET"} className="field">
                {httpMethods.map((m) => (
                  <option key={m}>{m}</option>
                ))}
              </select>
            </Field>
            <Field
              label={t("monitorForm.request.expectedStatus")}
              span={2}
              hint={t("monitorForm.request.expectedStatus.hint")}
            >
              <input
                name="http_expected_status"
                defaultValue={http?.expected_status ?? "200-399"}
                placeholder={t("monitorForm.request.expectedStatus.placeholder")}
                className="field font-mono"
              />
            </Field>
            <Field
              label={t("monitorForm.request.maxRedirects")}
              span={2}
              hint={
                redirectCount === null
                  ? t("monitorForm.request.maxRedirects.hint")
                  : redirectCount === 0
                    ? t("monitorForm.echo.redirects.none")
                    : t.n("monitorForm.echo.redirects", redirectCount)
              }
            >
              <input
                name="http_max_redirects"
                type="number"
                inputMode="numeric"
                min={0}
                max={20}
                value={redirects}
                onChange={(e) => setRedirects(e.target.value)}
                className="field tabular-nums"
              />
            </Field>
            <div className="mon-span-6">
              <span className="mon-label">{t("monitorForm.request.auth")}</span>
              <Segmented
                name="http_auth"
                label={t("monitorForm.request.auth")}
                value={auth}
                onChange={(v) => setAuth(v)}
                options={[
                  { value: "", label: t("monitorForm.request.auth.none") },
                  { value: "basic", label: t("monitorForm.request.auth.basic") },
                  { value: "bearer", label: t("monitorForm.request.auth.bearer") },
                ]}
              />
            </div>
            {auth === "basic" && (
              <Field label={t("monitorForm.request.user")} span={3}>
                <input name="http_auth_user" defaultValue={http?.auth_user} autoComplete="off" className="field" />
              </Field>
            )}
            {auth !== "" && (
              <Field
                label={auth === "basic" ? t("monitorForm.request.password") : t("monitorForm.request.token")}
                span={auth === "basic" ? 3 : 6}
                hint={http?.auth_secret ? t("monitorForm.secret.keptHint") : undefined}
              >
                <input
                  name="http_auth_secret"
                  type="password"
                  // The stored secret never reaches the browser; the mask stands for it.
                  defaultValue={http?.auth === auth ? http?.auth_secret : ""}
                  autoComplete="new-password"
                  className="field font-mono"
                />
              </Field>
            )}
            <Field
              label={t("monitorForm.request.headers")}
              span={6}
              hint={t("monitorForm.request.headers.hint", { mask: MASKED_VALUE })}
            >
              <textarea
                name="http_headers"
                rows={Math.max(2, Object.keys(http?.headers ?? {}).length + 1)}
                defaultValue={Object.entries(http?.headers ?? {})
                  .map(([name, value]) => `${name}: ${value}`)
                  .join("\n")}
                placeholder={t("monitorForm.request.headers.placeholder")}
                className="field font-mono"
              />
            </Field>
            <Field label={t("monitorForm.request.body")} optional={optional} span={6}>
              <textarea
                name="http_body"
                rows={3}
                defaultValue={http?.body}
                placeholder={t("monitorForm.request.body.placeholder")}
                className="field font-mono"
              />
            </Field>
            <div className="mon-span-6">
              <span className="mon-label">{t("monitorForm.request.contentType")}</span>
              <Segmented
                name="http_content_type"
                label={t("monitorForm.request.contentType")}
                defaultValue={http?.content_type || "application/json"}
                options={contentTypes.map((c) => ({ value: c.value, label: t(c.label) }))}
              />
            </div>
            <div className="mon-span-6">
              <Check
                name="tls_skip_verify"
                label={t("monitorForm.tls.skipVerify")}
                hint={t("monitorForm.tls.skipVerify.hint")}
                defaultChecked={http?.tls_skip_verify}
              />
            </div>
          </Section>

          <Section title={t("monitorForm.response.title")} hint={t("monitorForm.response.hint")}>
            <Field label={t("monitorForm.response.keyword")} optional={optional} span={6}>
              <input
                name="http_keyword"
                defaultValue={http?.keyword}
                placeholder={t("monitorForm.response.keyword.placeholder")}
                className="field font-mono"
              />
            </Field>
            <div className="mon-span-6">
              <span className="mon-label">{t("monitorForm.response.condition")}</span>
              <Segmented
                name="http_invert_keyword"
                label={t("monitorForm.response.condition")}
                defaultValue={http?.invert_keyword ? "on" : ""}
                options={[
                  { value: "", label: t("monitorForm.response.condition.present") },
                  { value: "on", label: t("monitorForm.response.condition.absent") },
                ]}
              />
            </div>
            <Field label={t("monitorForm.response.jsonPath")} optional={optional} span={2} hint={t("monitorForm.response.jsonPath.hint")}>
              <input
                name="http_json_path"
                defaultValue={http?.json_path}
                placeholder={t("monitorForm.response.jsonPath.placeholder")}
                className="field font-mono"
              />
            </Field>
            <Field label={t("monitorForm.response.operator")} span={2}>
              <select name="http_json_operator" defaultValue={http?.json_operator || "=="} className="field">
                {jsonOperators.map((o) => (
                  <option key={o.value} value={o.value}>
                    {t(o.label)}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={t("monitorForm.response.expected")} span={2}>
              <input
                name="http_json_expected"
                defaultValue={http?.json_expected}
                placeholder={t("monitorForm.response.expected.placeholder")}
                className="field font-mono"
              />
            </Field>
          </Section>
        </>
      )}

      {type === "tcp" && (
        <Section title={t("monitorForm.tcp.title")} hint={t("monitorForm.tcp.hint")}>
          <div className="mon-span-6 flex flex-col gap-4">
            <Check
              name="tcp_tls"
              label={t("monitorForm.tcp.tls")}
              hint={t("monitorForm.tcp.tls.hint")}
              defaultChecked={monitor?.tcp?.tls}
            />
            <Check name="tls_skip_verify" label={t("monitorForm.tls.skipVerify")} defaultChecked={monitor?.tcp?.tls_skip_verify} />
          </div>
        </Section>
      )}

      {type === "dns" && (
        <Section title={t("monitorForm.dns.title")} hint={t("monitorForm.dns.hint")}>
          <Field label={t("monitorForm.dns.recordType")} span={2}>
            <select name="dns_record_type" defaultValue={monitor?.dns?.record_type ?? "A"} className="field">
              {dnsRecordTypes.map((rt) => (
                <option key={rt}>{rt}</option>
              ))}
            </select>
          </Field>
          <Field label={t("monitorForm.dns.resolver")} optional={optional} span={4} hint={t("monitorForm.dns.resolver.hint")}>
            <input
              name="dns_resolver"
              defaultValue={monitor?.dns?.resolver}
              placeholder={t("monitorForm.dns.resolver.placeholder")}
              className="field font-mono"
            />
          </Field>
          <Field label={t("monitorForm.dns.expected")} optional={optional} span={6} hint={t("monitorForm.dns.expected.hint")}>
            <input
              name="dns_expected"
              defaultValue={monitor?.dns?.expected}
              placeholder={t("monitorForm.dns.expected.placeholder")}
              className="field font-mono"
            />
          </Field>
        </Section>
      )}

      {type === "grpc" && (
        <Section title={t("monitorForm.grpc.title")} hint={t("monitorForm.grpc.hint")}>
          <Field label={t("monitorForm.grpc.service")} optional={optional} span={6} hint={t("monitorForm.grpc.service.hint")}>
            <input
              name="grpc_service"
              defaultValue={monitor?.grpc?.service}
              placeholder={t("monitorForm.grpc.service.placeholder")}
              className="field font-mono"
            />
          </Field>
          <div className="mon-span-6 flex flex-col gap-4">
            <Check name="grpc_tls" label={t("monitorForm.tls.use")} defaultChecked={monitor?.grpc?.tls} />
            <Check name="tls_skip_verify" label={t("monitorForm.tls.skipVerify")} defaultChecked={monitor?.grpc?.tls_skip_verify} />
          </div>
        </Section>
      )}

      {isDatabaseType(type) && (
        <Section
          title={t("monitorForm.credentials.title")}
          hint={type === "redis" ? t("monitorForm.credentials.redisHint") : t("monitorForm.credentials.hint")}
        >
          <Field label={t("monitorForm.credentials.user")} optional={type === "redis" ? optional : undefined} span={3}>
            <input
              name="db_username"
              required={type !== "redis"}
              defaultValue={monitor?.database?.username}
              autoComplete="off"
              className="field"
            />
          </Field>
          <Field
            label={t("monitorForm.credentials.password")}
            span={3}
            hint={monitor?.database?.password ? t("monitorForm.secret.keptHint") : undefined}
          >
            <input
              name="db_password"
              type="password"
              // The stored password never reaches the browser; the mask stands for it.
              defaultValue={monitor?.database?.password}
              autoComplete="new-password"
              className="field font-mono"
            />
          </Field>
          {type !== "redis" && (
            <>
              <Field label={t("monitorForm.credentials.database")} optional={optional} span={2}>
                <input
                  name="db_database"
                  defaultValue={monitor?.database?.database}
                  placeholder={t("monitorForm.credentials.database.placeholder")}
                  className="field font-mono"
                />
              </Field>
              <Field label={t("monitorForm.credentials.query")} span={4} hint={t("monitorForm.credentials.query.hint")}>
                <input
                  name="db_query"
                  defaultValue={monitor?.database?.query || "SELECT 1"}
                  maxLength={1000}
                  className="field font-mono"
                />
              </Field>
            </>
          )}
          <div className="mon-span-6 flex flex-col gap-4">
            <Check name="db_tls" label={t("monitorForm.tls.use")} defaultChecked={monitor?.database?.tls} />
            <Check name="tls_skip_verify" label={t("monitorForm.tls.skipVerify")} defaultChecked={monitor?.database?.tls_skip_verify} />
          </div>
        </Section>
      )}

      {type === "workload" && (
        <Section title={t("monitorForm.workload.title")} hint={t("monitorForm.workload.hint")}>
          <Field
            label={t("monitorForm.workload.minReady")}
            span={3}
            hint={
              minReadyCount === null
                ? t("monitorForm.workload.minReady.hint")
                : minReadyCount === 0
                  ? t("monitorForm.echo.minReady.all")
                  : t.n("monitorForm.echo.minReady", minReadyCount)
            }
          >
            <input
              name="workload_min_ready"
              type="number"
              inputMode="numeric"
              min={0}
              value={minReady}
              onChange={(e) => setMinReady(e.target.value)}
              className="field tabular-nums"
            />
          </Field>
        </Section>
      )}

      {type === "websocket" && (
        <Section title={t("monitorForm.websocket.title")} hint={t("monitorForm.websocket.hint")}>
          <Field
            label={t("monitorForm.websocket.subprotocol")}
            optional={optional}
            span={6}
            hint={t("monitorForm.websocket.subprotocol.hint")}
          >
            <input
              name="ws_subprotocol"
              defaultValue={monitor?.websocket?.subprotocol}
              placeholder={t("monitorForm.websocket.subprotocol.placeholder")}
              className="field font-mono"
            />
          </Field>
          <div className="mon-span-6">
            <Check
              name="tls_skip_verify"
              label={t("monitorForm.tls.skipVerify")}
              defaultChecked={monitor?.websocket?.tls_skip_verify}
            />
          </div>
        </Section>
      )}

      {!push && (
        <Section title={t("monitorForm.advanced.title")} hint={t("monitorForm.advanced.hint")}>
          <div className="mon-span-6 flex flex-col gap-4">
            {fromService && (
              <Check
                name="each_pod"
                label={t("monitorForm.advanced.eachPod")}
                hint={managed ? t("monitorForm.advanced.eachPod.managedHint") : t("monitorForm.advanced.eachPod.hint")}
                defaultChecked={monitor?.each_pod}
                disabled={managed}
              />
            )}
            <Check
              name="inverted"
              label={t("monitorForm.advanced.inverted")}
              hint={t("monitorForm.advanced.inverted.hint")}
              defaultChecked={monitor?.inverted}
            />
          </div>
        </Section>
      )}

      <div className="mon-savebar">
        {error && (
          <p role="alert" className="min-w-0 flex-1 text-[13px] text-down-ink">
            {error}
          </p>
        )}
        <div className="mon-savebar-end">
          <Link href={monitor ? `/monitors/${monitor.id}` : "/"} className="btn btn-ghost">
            {t("common.cancel")}
          </Link>
          <button type="submit" disabled={pending} className="btn btn-primary px-5">
            {pending ? t("common.saving") : monitor ? t("common.save") : t("monitorForm.create")}
          </button>
        </div>
      </div>
    </form>
  );
}
