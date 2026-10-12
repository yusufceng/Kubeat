"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { saveMonitorAction } from "@/app/actions";
import type { DnsRecordType, HttpMethod, JsonOperator, Monitor, MonitorType } from "@/lib/api";
import { MASKED_VALUE } from "@/lib/constants";
import { isDatabaseType, monitorTypeOrder, monitorTypes } from "@/lib/monitor-types";

const httpMethods: HttpMethod[] = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"];
const jsonOperators: { value: JsonOperator; label: string }[] = [
  { value: "==", label: "eşittir" },
  { value: "!=", label: "eşit değildir" },
  { value: ">", label: "büyüktür" },
  { value: ">=", label: "büyük ya da eşittir" },
  { value: "<", label: "küçüktür" },
  { value: "<=", label: "küçük ya da eşittir" },
  { value: "contains", label: "içerir" },
];
const dnsRecordTypes: DnsRecordType[] = ["A", "AAAA", "CNAME", "MX", "NS", "TXT", "SRV", "PTR"];
const contentTypes = [
  { value: "application/json", label: "JSON" },
  { value: "application/x-www-form-urlencoded", label: "Form" },
  { value: "application/xml", label: "XML" },
  { value: "text/plain", label: "Düz metin" },
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

function Check({ name, label, hint, defaultChecked }: { name: string; label: string; hint?: string; defaultChecked?: boolean }) {
  return (
    <label className="flex items-start gap-2.5 text-sm sm:col-span-6">
      <input type="checkbox" name={name} defaultChecked={defaultChecked} className="mt-0.5 h-4 w-4 accent-(--accent)" />
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
  const [type, setType] = useState<MonitorType>(monitor?.type ?? "http");
  const [auth, setAuth] = useState(monitor?.http?.auth ?? "");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();

  const info = monitorTypes[type];
  const http = monitor?.http;
  // A discovered monitor is named after the object it was made from.
  const discovered = (monitor?.source ?? "") !== "";

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

      <Section title="Genel">
        {monitor ? (
          <Field label="Tip" span={2}>
            <input defaultValue={info.label} disabled className="field" />
          </Field>
        ) : (
          <Field label="Tip" span={2} hint={info.summary}>
            <select value={type} onChange={(e) => setType(e.target.value as MonitorType)} className="field">
              {monitorTypeOrder.map((t) => (
                <option key={t} value={t}>
                  {monitorTypes[t].label}
                </option>
              ))}
            </select>
          </Field>
        )}
        {discovered ? (
          <Field label="Ad" span={4} hint="Keşfedilen monitor'lar adını cluster'daki nesneden alır.">
            <input defaultValue={monitor?.name} disabled className="field" />
          </Field>
        ) : (
          <Field label="Ad" span={4}>
            <input name="name" required defaultValue={monitor?.name} placeholder="odeme-api" className="field" />
          </Field>
        )}
        {info.target ? (
          <Field label={info.target.label} span={6}>
            <input
              name="target"
              required
              defaultValue={monitor?.target}
              placeholder={info.target.placeholder}
              className="field font-mono"
            />
          </Field>
        ) : (
          <p className="rounded-lg bg-sunken p-3 text-sm text-muted sm:col-span-6">
            Push monitor&apos;unda kontrol edilecek bir adres yoktur. Kaydettikten sonra monitor sayfasında
            bir rapor adresi görünür; izlenen iş (CronJob, yedekleme betiği) her çalıştığında o adrese bir
            istek atar. Beklenen sürede rapor gelmezse alarm üretilir.
          </p>
        )}
        <Field label="Açıklama (isteğe bağlı)" span={6}>
          <input
            name="description"
            defaultValue={monitor?.description}
            maxLength={500}
            placeholder="Neyi izliyor, kime ait?"
            className="field"
          />
        </Field>
        {discovered ? (
          <Field label="Namespace" span={3} hint="Keşfedilen monitor nesnesinin namespace'inde kalır.">
            <input defaultValue={monitor?.namespace} disabled className="field" />
          </Field>
        ) : (
          <Field label="Grup (isteğe bağlı)" span={3} hint="Listede bu ad altında toplanır; bildirim kanalları gruba göre sınırlanabilir.">
            <input
              name="group"
              list="monitor-groups"
              defaultValue={monitor?.namespace}
              maxLength={63}
              placeholder="Ödeme sistemleri"
              className="field"
            />
            <datalist id="monitor-groups">
              {groups.map((g) => (
                <option key={g} value={g} />
              ))}
            </datalist>
          </Field>
        )}
        <Field label="Etiketler (isteğe bağlı)" span={3} hint="Virgülle ayırın. Listede etikete göre süzebilirsiniz.">
          <input name="tags" defaultValue={monitor?.tags.join(", ")} placeholder="prod, kritik" className="field" />
        </Field>
      </Section>

      <Section
        title="Zamanlama ve alarm"
        hint={
          type === "push"
            ? "Raporun ne sıklıkta beklendiği ve ne kadar gecikmenin hoş görüleceği."
            : "Ne sıklıkta kontrol edileceği ve kaç hatadan sonra kesinti sayılacağı."
        }
      >
        <Field label={type === "push" ? "Rapor aralığı (sn)" : "Kontrol aralığı (sn)"} span={type === "push" ? 2 : 1}>
          <input
            name="interval_seconds"
            type="number"
            min={1}
            required
            defaultValue={monitor?.interval_seconds ?? defaults.interval}
            className="field"
          />
        </Field>
        <Field label={type === "push" ? "Gecikme payı (sn)" : "Zaman aşımı (sn)"} span={type === "push" ? 2 : 1}>
          <input
            name="timeout_seconds"
            type="number"
            min={1}
            required
            defaultValue={monitor?.timeout_seconds ?? defaults.timeout}
            className="field"
          />
        </Field>
        <Field
          label="Alarm eşiği"
          span={2}
          hint="Art arda bu kadar başarısız kontrol kesinti sayılır ve bildirim gider."
        >
          <input
            name="failure_threshold"
            type="number"
            min={1}
            required
            defaultValue={monitor?.failure_threshold ?? defaults.failureThreshold}
            className="field"
          />
        </Field>
        {type !== "push" && (
          <Field
            label="Tekrar aralığı (sn)"
            span={2}
            hint="Hata sonrası, kesinti kesinleşene kadar kontrolün ne kadar sonra tekrarlanacağı. 0: normal aralık."
          >
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
          <Section title="İstek">
            <Field label="Metot" span={2}>
              <select name="http_method" defaultValue={http?.method ?? "GET"} className="field">
                {httpMethods.map((m) => (
                  <option key={m}>{m}</option>
                ))}
              </select>
            </Field>
            <Field label="Kabul edilen durum kodları" span={2} hint="Kodlar ve aralıklar: 200-299,401">
              <input
                name="http_expected_status"
                defaultValue={http?.expected_status ?? "200-399"}
                placeholder="200-399"
                className="field font-mono"
              />
            </Field>
            <Field label="İzlenecek yönlendirme" span={2} hint="0: ilk yanıt değerlendirilir.">
              <input
                name="http_max_redirects"
                type="number"
                min={0}
                max={20}
                defaultValue={http?.max_redirects ?? 10}
                className="field"
              />
            </Field>
            <Field label="Kimlik doğrulama" span={2}>
              <select name="http_auth" value={auth} onChange={(e) => setAuth(e.target.value as typeof auth)} className="field">
                <option value="">Yok</option>
                <option value="basic">Basic (kullanıcı ve parola)</option>
                <option value="bearer">Bearer token</option>
              </select>
            </Field>
            {auth === "basic" && (
              <Field label="Kullanıcı" span={2}>
                <input name="http_auth_user" defaultValue={http?.auth_user} autoComplete="off" className="field" />
              </Field>
            )}
            {auth !== "" && (
              <Field
                label={auth === "basic" ? "Parola" : "Token"}
                span={auth === "basic" ? 2 : 4}
                hint={http?.auth_secret ? "Kayıtlı değer gösterilmez; değiştirmezseniz korunur." : undefined}
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
              label="Header'lar (satır başına bir tane, “Ad: değer”)"
              span={6}
              hint={`Kayıtlı değerler ${MASKED_VALUE} olarak gösterilir; satırı öyle bırakırsanız değer korunur.`}
            >
              <textarea
                name="http_headers"
                rows={Math.max(2, Object.keys(http?.headers ?? {}).length + 1)}
                defaultValue={Object.entries(http?.headers ?? {})
                  .map(([name, value]) => `${name}: ${value}`)
                  .join("\n")}
                placeholder="X-Tenant: shop"
                className="field font-mono"
              />
            </Field>
            <Field label="Gövde (isteğe bağlı)" span={4}>
              <textarea
                name="http_body"
                rows={3}
                defaultValue={http?.body}
                placeholder='{"deep": true}'
                className="field font-mono"
              />
            </Field>
            <Field label="Gövde tipi" span={2}>
              <select name="http_content_type" defaultValue={http?.content_type || "application/json"} className="field">
                {contentTypes.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </Field>
            <Check
              name="tls_skip_verify"
              label="TLS sertifikasını doğrulama"
              hint="Özel CA kullanan hedefler için. Sertifikanın bitiş tarihi yine izlenir."
              defaultChecked={http?.tls_skip_verify}
            />
          </Section>

          <Section title="Yanıtta aranacaklar" hint="Boş bırakılırsa yalnızca durum koduna bakılır.">
            <Field label="Metin" span={4}>
              <input name="http_keyword" defaultValue={http?.keyword} placeholder='"status":"ok"' className="field font-mono" />
            </Field>
            <Field label="Koşul" span={2}>
              <select name="http_invert_keyword" defaultValue={http?.invert_keyword ? "on" : ""} className="field">
                <option value="">gövdede bulunmalı</option>
                <option value="on">gövdede bulunmamalı</option>
              </select>
            </Field>
            <Field label="JSON yolu" span={2} hint="Örnek: status, checks[0].state">
              <input name="http_json_path" defaultValue={http?.json_path} placeholder="status" className="field font-mono" />
            </Field>
            <Field label="Karşılaştırma" span={2}>
              <select name="http_json_operator" defaultValue={http?.json_operator || "=="} className="field">
                {jsonOperators.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Beklenen değer" span={2}>
              <input name="http_json_expected" defaultValue={http?.json_expected} placeholder="ok" className="field font-mono" />
            </Field>
          </Section>
        </>
      )}

      {type === "tcp" && (
        <Section title="TCP ayarları">
          <Check
            name="tcp_tls"
            label="TLS el sıkışması yap"
            hint="Bağlantıdan sonra TLS kurulur; sertifikanın bitiş tarihi izlenir ve süresi yaklaşınca uyarı üretilir."
            defaultChecked={monitor?.tcp?.tls}
          />
          <Check name="tls_skip_verify" label="TLS sertifikasını doğrulama" defaultChecked={monitor?.tcp?.tls_skip_verify} />
        </Section>
      )}

      {type === "dns" && (
        <Section title="DNS ayarları">
          <Field label="Kayıt tipi" span={2}>
            <select name="dns_record_type" defaultValue={monitor?.dns?.record_type ?? "A"} className="field">
              {dnsRecordTypes.map((t) => (
                <option key={t}>{t}</option>
              ))}
            </select>
          </Field>
          <Field label="Çözümleyici (isteğe bağlı)" span={4} hint="Boşsa Kubeat'in çalıştığı ortamın DNS'i kullanılır.">
            <input
              name="dns_resolver"
              defaultValue={monitor?.dns?.resolver}
              placeholder="10.96.0.10 ya da ns1.example.com:53"
              className="field font-mono"
            />
          </Field>
          <Field
            label="Beklenen değer (isteğe bağlı)"
            span={6}
            hint="Bulunan kayıtlardan en az biri bu metni içermelidir."
          >
            <input name="dns_expected" defaultValue={monitor?.dns?.expected} placeholder="10.0.0.5" className="field font-mono" />
          </Field>
        </Section>
      )}

      {type === "grpc" && (
        <Section title="gRPC ayarları">
          <Field label="Servis adı (isteğe bağlı)" span={6} hint="Boşsa sunucunun genel durumu sorulur.">
            <input name="grpc_service" defaultValue={monitor?.grpc?.service} placeholder="shop.Cart" className="field font-mono" />
          </Field>
          <Check name="grpc_tls" label="TLS kullan" defaultChecked={monitor?.grpc?.tls} />
          <Check name="tls_skip_verify" label="TLS sertifikasını doğrulama" defaultChecked={monitor?.grpc?.tls_skip_verify} />
        </Section>
      )}

      {isDatabaseType(type) && (
        <Section
          title="Giriş bilgileri"
          hint={
            type === "redis"
              ? "Sunucu parola istemiyorsa boş bırakın. Kullanıcı adı yalnızca ACL kullanan sunucular içindir."
              : "Yalnızca okuma yetkisi olan bir kullanıcı yeterlidir."
          }
        >
          <Field label={type === "redis" ? "Kullanıcı (isteğe bağlı)" : "Kullanıcı"} span={3}>
            <input
              name="db_username"
              required={type !== "redis"}
              defaultValue={monitor?.database?.username}
              autoComplete="off"
              className="field"
            />
          </Field>
          <Field
            label="Parola"
            span={3}
            hint={monitor?.database?.password ? "Kayıtlı değer gösterilmez; değiştirmezseniz korunur." : undefined}
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
              <Field label="Veritabanı (isteğe bağlı)" span={2}>
                <input name="db_database" defaultValue={monitor?.database?.database} placeholder="orders" className="field font-mono" />
              </Field>
              <Field label="Sorgu" span={4} hint="Giriş yaptıktan sonra çalıştırılır; hata verirse kontrol başarısızdır.">
                <input
                  name="db_query"
                  defaultValue={monitor?.database?.query || "SELECT 1"}
                  maxLength={1000}
                  className="field font-mono"
                />
              </Field>
            </>
          )}
          <Check name="db_tls" label="TLS kullan" defaultChecked={monitor?.database?.tls} />
          <Check name="tls_skip_verify" label="TLS sertifikasını doğrulama" defaultChecked={monitor?.database?.tls_skip_verify} />
        </Section>
      )}

      {type === "websocket" && (
        <Section title="WebSocket ayarları">
          <Field label="Alt protokol (isteğe bağlı)" span={6} hint="Verilirse sunucunun onu kabul etmesi gerekir.">
            <input
              name="ws_subprotocol"
              defaultValue={monitor?.websocket?.subprotocol}
              placeholder="graphql-ws"
              className="field font-mono"
            />
          </Field>
          <Check
            name="tls_skip_verify"
            label="TLS sertifikasını doğrulama"
            defaultChecked={monitor?.websocket?.tls_skip_verify}
          />
        </Section>
      )}

      {type !== "push" && (
        <Section title="Gelişmiş">
          <Check
            name="inverted"
            label="Ters mod"
            hint="Sonucu tersine çevirir: hedef başarısız olduğu sürece monitor çalışıyor sayılır. Erişilmemesi gereken şeyler için."
            defaultChecked={monitor?.inverted}
          />
        </Section>
      )}

      <div className="flex flex-wrap items-center gap-3">
        <button type="submit" disabled={pending} className="btn btn-primary px-5">
          {pending ? "Kaydediliyor…" : monitor ? "Kaydet" : "Monitor'u oluştur"}
        </button>
        <Link href={monitor ? `/monitors/${monitor.id}` : "/"} className="btn">
          Vazgeç
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
