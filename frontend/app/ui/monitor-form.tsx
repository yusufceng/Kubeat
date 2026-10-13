"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { saveMonitorAction } from "@/app/actions";
import type { DnsRecordType, HttpMethod, JsonOperator, Monitor, MonitorType } from "@/lib/api";
import { MASKED_VALUE } from "@/lib/constants";
import { useT } from "@/lib/i18n/client";
import { isDatabaseType, monitorTypeOrder, monitorTypes } from "@/lib/monitor-types";

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

export type MonitorDefaults = { interval: number; timeout: number; failureThreshold: number };

function Section({ title, hint, children }: { title: string; hint?: string; children: React.ReactNode }) {
  return (
    <section className="card p-5" aria-label={title}>
      <h2 className="text-sm font-semibold">{title}</h2>
      {hint && <p className="mt-0.5 text-xs text-muted">{hint}</p>}
      <div className="mt-4 grid gap-4 sm:grid-cols-6">{children}</div>
    </section>
  );
}

function Field({
  label,
  hint,
  span = 3,
  children,
}: {
  label: string;
  hint?: React.ReactNode;
  /** Columns out of six. */
  span?: 1 | 2 | 3 | 4 | 6;
  children: React.ReactNode;
}) {
  const spans = { 1: "sm:col-span-1", 2: "sm:col-span-2", 3: "sm:col-span-3", 4: "sm:col-span-4", 6: "sm:col-span-6" };
  return (
    <label className={`block ${spans[span]}`}>
      <span className="mb-1 block text-xs font-medium text-muted">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-muted">{hint}</span>}
    </label>
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
    <label className="flex items-start gap-2.5 text-sm sm:col-span-6">
      <input
        type="checkbox"
        name={name}
        defaultChecked={defaultChecked}
        disabled={disabled}
        className="mt-0.5 h-4 w-4 accent-(--accent)"
      />
      <span>
        {label}
        {hint && <span className="block text-xs text-muted">{hint}</span>}
      </span>
    </label>
  );
}

/**
 * The form of a monitor: every setting of every type. With a monitor it
 * edits that one; without, it creates one of the type chosen at the top.
 */
export function MonitorForm({
  monitor,
  defaults,
  groups,
}: {
  monitor?: Monitor;
  defaults: MonitorDefaults;
  /** The groups that exist, offered as suggestions. */
  groups: string[];
}) {
  const t = useT();
  const [type, setType] = useState<MonitorType>(monitor?.type ?? "http");
  const [auth, setAuth] = useState(monitor?.http?.auth ?? "");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  const info = monitorTypes[type];
  const http = monitor?.http;
  // A discovered monitor is named after the object it was made from. One
  // made from an annotation also takes its target and timing from there.
  const discovered = (monitor?.source ?? "") !== "";
  const managed = monitor?.managed ?? false;
  // Only a service port has pods to check one by one.
  const fromService = (monitor?.source ?? "").startsWith("service/") && (type === "http" || type === "tcp");
  /** A field's label with "(optional)" after it. */
  const optional = (key: string) => `${t(key)} ${t("common.optional")}`;

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

  return (
    <form onSubmit={submit} className="flex flex-col gap-5">
      {monitor && <input type="hidden" name="id" value={monitor.id} />}
      <input type="hidden" name="type" value={type} />

      <Section title={t("monitorForm.general.title")}>
        {monitor ? (
          <Field label={t("monitorForm.general.type")} span={2}>
            <input defaultValue={t(info.label)} disabled className="field" />
          </Field>
        ) : (
          <Field label={t("monitorForm.general.type")} span={2} hint={t(info.summary)}>
            <select value={type} onChange={(e) => setType(e.target.value as MonitorType)} className="field">
              {monitorTypeOrder.map((mt) => (
                <option key={mt} value={mt}>
                  {t(monitorTypes[mt].label)}
                </option>
              ))}
            </select>
          </Field>
        )}
        {discovered ? (
          <Field label={t("monitorForm.general.name")} span={4} hint={t("monitorForm.general.name.discoveredHint")}>
            <input defaultValue={monitor?.name} disabled className="field" />
          </Field>
        ) : (
          <Field label={t("monitorForm.general.name")} span={4}>
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
              defaultValue={monitor?.target}
              placeholder={t(info.target.placeholder)}
              className="field font-mono"
            />
          </Field>
        ) : (
          <p className="rounded-lg bg-sunken p-3 text-sm text-muted sm:col-span-6">{t("monitorForm.general.pushNote")}</p>
        )}
        <Field label={optional("monitorForm.general.description")} span={6}>
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
            <input defaultValue={monitor?.namespace} disabled className="field" />
          </Field>
        ) : (
          <Field label={optional("monitorForm.general.group")} span={3} hint={t("monitorForm.general.group.hint")}>
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
        <Field label={optional("monitorForm.general.tags")} span={3} hint={t("monitorForm.general.tags.hint")}>
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
        hint={
          managed
            ? t("monitorForm.timing.managedHint")
            : type === "push"
              ? t("monitorForm.timing.pushHint")
              : t("monitorForm.timing.hint")
        }
      >
        <Field
          label={type === "push" ? t("monitorForm.timing.reportInterval") : t("monitorForm.timing.interval")}
          span={type === "push" ? 2 : 1}
        >
          <input
            name="interval_seconds"
            type="number"
            min={1}
            required
            readOnly={managed}
            defaultValue={monitor?.interval_seconds ?? defaults.interval}
            className="field"
          />
        </Field>
        <Field
          label={type === "push" ? t("monitorForm.timing.gracePeriod") : t("monitorForm.timing.timeout")}
          span={type === "push" ? 2 : 1}
        >
          <input
            name="timeout_seconds"
            type="number"
            min={1}
            required
            readOnly={managed}
            defaultValue={monitor?.timeout_seconds ?? defaults.timeout}
            className="field"
          />
        </Field>
        <Field label={t("monitorForm.timing.threshold")} span={2} hint={t("monitorForm.timing.threshold.hint")}>
          <input
            name="failure_threshold"
            type="number"
            min={1}
            required
            readOnly={managed}
            defaultValue={monitor?.failure_threshold ?? defaults.failureThreshold}
            className="field"
          />
        </Field>
        {type !== "push" && (
          <Field label={t("monitorForm.timing.retryInterval")} span={2} hint={t("monitorForm.timing.retryInterval.hint")}>
            <input
              name="retry_interval_seconds"
              type="number"
              min={0}
              defaultValue={monitor?.retry_interval_seconds ?? 0}
              className="field"
            />
          </Field>
        )}
      </Section>

      {type === "http" && (
        <>
          <Section title={t("monitorForm.request.title")}>
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
            <Field label={t("monitorForm.request.maxRedirects")} span={2} hint={t("monitorForm.request.maxRedirects.hint")}>
              <input
                name="http_max_redirects"
                type="number"
                min={0}
                max={20}
                defaultValue={http?.max_redirects ?? 10}
                className="field"
              />
            </Field>
            <Field label={t("monitorForm.request.auth")} span={2}>
              <select name="http_auth" value={auth} onChange={(e) => setAuth(e.target.value as typeof auth)} className="field">
                <option value="">{t("monitorForm.request.auth.none")}</option>
                <option value="basic">{t("monitorForm.request.auth.basic")}</option>
                <option value="bearer">{t("monitorForm.request.auth.bearer")}</option>
              </select>
            </Field>
            {auth === "basic" && (
              <Field label={t("monitorForm.request.user")} span={2}>
                <input name="http_auth_user" defaultValue={http?.auth_user} autoComplete="off" className="field" />
              </Field>
            )}
            {auth !== "" && (
              <Field
                label={auth === "basic" ? t("monitorForm.request.password") : t("monitorForm.request.token")}
                span={auth === "basic" ? 2 : 4}
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
            <Field label={optional("monitorForm.request.body")} span={4}>
              <textarea
                name="http_body"
                rows={3}
                defaultValue={http?.body}
                placeholder={t("monitorForm.request.body.placeholder")}
                className="field font-mono"
              />
            </Field>
            <Field label={t("monitorForm.request.contentType")} span={2}>
              <select name="http_content_type" defaultValue={http?.content_type || "application/json"} className="field">
                {contentTypes.map((c) => (
                  <option key={c.value} value={c.value}>
                    {t(c.label)}
                  </option>
                ))}
              </select>
            </Field>
            <Check
              name="tls_skip_verify"
              label={t("monitorForm.tls.skipVerify")}
              hint={t("monitorForm.tls.skipVerify.hint")}
              defaultChecked={http?.tls_skip_verify}
            />
          </Section>

          <Section title={t("monitorForm.response.title")} hint={t("monitorForm.response.hint")}>
            <Field label={t("monitorForm.response.keyword")} span={4}>
              <input
                name="http_keyword"
                defaultValue={http?.keyword}
                placeholder={t("monitorForm.response.keyword.placeholder")}
                className="field font-mono"
              />
            </Field>
            <Field label={t("monitorForm.response.condition")} span={2}>
              <select name="http_invert_keyword" defaultValue={http?.invert_keyword ? "on" : ""} className="field">
                <option value="">{t("monitorForm.response.condition.present")}</option>
                <option value="on">{t("monitorForm.response.condition.absent")}</option>
              </select>
            </Field>
            <Field label={t("monitorForm.response.jsonPath")} span={2} hint={t("monitorForm.response.jsonPath.hint")}>
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
        <Section title={t("monitorForm.tcp.title")}>
          <Check
            name="tcp_tls"
            label={t("monitorForm.tcp.tls")}
            hint={t("monitorForm.tcp.tls.hint")}
            defaultChecked={monitor?.tcp?.tls}
          />
          <Check name="tls_skip_verify" label={t("monitorForm.tls.skipVerify")} defaultChecked={monitor?.tcp?.tls_skip_verify} />
        </Section>
      )}

      {type === "dns" && (
        <Section title={t("monitorForm.dns.title")}>
          <Field label={t("monitorForm.dns.recordType")} span={2}>
            <select name="dns_record_type" defaultValue={monitor?.dns?.record_type ?? "A"} className="field">
              {dnsRecordTypes.map((rt) => (
                <option key={rt}>{rt}</option>
              ))}
            </select>
          </Field>
          <Field label={optional("monitorForm.dns.resolver")} span={4} hint={t("monitorForm.dns.resolver.hint")}>
            <input
              name="dns_resolver"
              defaultValue={monitor?.dns?.resolver}
              placeholder={t("monitorForm.dns.resolver.placeholder")}
              className="field font-mono"
            />
          </Field>
          <Field label={optional("monitorForm.dns.expected")} span={6} hint={t("monitorForm.dns.expected.hint")}>
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
        <Section title={t("monitorForm.grpc.title")}>
          <Field label={optional("monitorForm.grpc.service")} span={6} hint={t("monitorForm.grpc.service.hint")}>
            <input
              name="grpc_service"
              defaultValue={monitor?.grpc?.service}
              placeholder={t("monitorForm.grpc.service.placeholder")}
              className="field font-mono"
            />
          </Field>
          <Check name="grpc_tls" label={t("monitorForm.tls.use")} defaultChecked={monitor?.grpc?.tls} />
          <Check name="tls_skip_verify" label={t("monitorForm.tls.skipVerify")} defaultChecked={monitor?.grpc?.tls_skip_verify} />
        </Section>
      )}

      {isDatabaseType(type) && (
        <Section
          title={t("monitorForm.credentials.title")}
          hint={type === "redis" ? t("monitorForm.credentials.redisHint") : t("monitorForm.credentials.hint")}
        >
          <Field
            label={type === "redis" ? optional("monitorForm.credentials.user") : t("monitorForm.credentials.user")}
            span={3}
          >
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
              <Field label={optional("monitorForm.credentials.database")} span={2}>
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
          <Check name="db_tls" label={t("monitorForm.tls.use")} defaultChecked={monitor?.database?.tls} />
          <Check name="tls_skip_verify" label={t("monitorForm.tls.skipVerify")} defaultChecked={monitor?.database?.tls_skip_verify} />
        </Section>
      )}

      {type === "workload" && (
        <Section title={t("monitorForm.workload.title")} hint={t("monitorForm.workload.hint")}>
          <Field label={t("monitorForm.workload.minReady")} span={3} hint={t("monitorForm.workload.minReady.hint")}>
            <input
              name="workload_min_ready"
              type="number"
              min={0}
              defaultValue={monitor?.workload?.min_ready ?? 0}
              className="field"
            />
          </Field>
        </Section>
      )}

      {type === "websocket" && (
        <Section title={t("monitorForm.websocket.title")}>
          <Field
            label={optional("monitorForm.websocket.subprotocol")}
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
          <Check
            name="tls_skip_verify"
            label={t("monitorForm.tls.skipVerify")}
            defaultChecked={monitor?.websocket?.tls_skip_verify}
          />
        </Section>
      )}

      {type !== "push" && (
        <Section title={t("monitorForm.advanced.title")}>
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
        </Section>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <button type="submit" disabled={pending} className="btn btn-primary px-5">
          {pending ? t("common.saving") : monitor ? t("common.save") : t("monitorForm.create")}
        </button>
        <Link href={monitor ? `/monitors/${monitor.id}` : "/"} className="btn">
          {t("common.cancel")}
        </Link>
        {error && (
          <p role="alert" className="text-sm text-down">
            {error}
          </p>
        )}
      </div>
    </form>
  );
}
