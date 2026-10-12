"use client";

import { useActionState } from "react";
import {
  deleteMonitorAction,
  type EditState,
  setMonitorEnabledAction,
  updateMonitorAction,
} from "@/app/actions";
import type { Monitor } from "@/lib/api";
import { MASKED_VALUE } from "@/lib/constants";

const inputClass =
  "w-full rounded-md border border-black/15 bg-transparent px-3 py-2 text-sm outline-none focus:border-emerald-600 dark:border-white/20";
const buttonClass =
  "rounded-md border border-black/15 px-3 py-1.5 text-sm hover:bg-black/5 dark:border-white/20 dark:hover:bg-white/10";

/** Pause/resume and delete buttons of a monitor. */
export function MonitorActions({ monitor }: { monitor: Pick<Monitor, "id" | "name" | "enabled"> }) {
  return (
    <div className="flex items-center gap-2">
      <form action={setMonitorEnabledAction}>
        <input type="hidden" name="id" value={monitor.id} />
        <input type="hidden" name="enabled" value={String(!monitor.enabled)} />
        <button type="submit" className={buttonClass}>
          {monitor.enabled ? "Durdur" : "Devam ettir"}
        </button>
      </form>
      <form
        action={deleteMonitorAction}
        onSubmit={(e) => {
          if (!confirm(`"${monitor.name}" ve tüm kontrol geçmişi silinsin mi?`)) e.preventDefault();
        }}
      >
        <input type="hidden" name="id" value={monitor.id} />
        <button
          type="submit"
          className="rounded-md border border-red-600/40 px-3 py-1.5 text-sm text-red-600 hover:bg-red-600/10 dark:text-red-400"
        >
          Sil
        </button>
      </form>
    </div>
  );
}

export function EditMonitorForm({ monitor }: { monitor: Monitor }) {
  const [state, action, pending] = useActionState<EditState, FormData>(updateMonitorAction, null);
  const discovered = monitor.source !== "";

  return (
    // Keyed on the saved values so the fields show them after a save or when
    // someone else changes the monitor.
    <form
      key={JSON.stringify([
        monitor.name,
        monitor.target,
        monitor.interval_seconds,
        monitor.timeout_seconds,
        monitor.failure_threshold,
        monitor.http,
      ])}
      action={action}
      className="grid gap-4 sm:grid-cols-6"
    >
      <input type="hidden" name="id" value={monitor.id} />
      {!discovered && (
        <label className="sm:col-span-2">
          <span className="mb-1 block text-xs font-medium text-zinc-500">İsim</span>
          <input name="name" required defaultValue={monitor.name} className={inputClass} />
        </label>
      )}
      <label className={discovered ? "sm:col-span-6" : "sm:col-span-4"}>
        <span className="mb-1 block text-xs font-medium text-zinc-500">
          Hedef ({monitor.type.toUpperCase()})
        </span>
        <input name="target" required defaultValue={monitor.target} className={inputClass} />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Kontrol aralığı (sn)</span>
        <input
          name="interval_seconds"
          type="number"
          min={1}
          required
          defaultValue={monitor.interval_seconds}
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-1">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Zaman aşımı (sn)</span>
        <input
          name="timeout_seconds"
          type="number"
          min={1}
          required
          defaultValue={monitor.timeout_seconds}
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-1">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Alarm eşiği</span>
        <input
          name="failure_threshold"
          type="number"
          min={1}
          required
          defaultValue={monitor.failure_threshold}
          className={inputClass}
        />
      </label>
      {monitor.http && (
        <fieldset className="grid gap-4 border-t border-black/10 pt-4 sm:col-span-6 sm:grid-cols-6 dark:border-white/15">
          <legend className="sr-only">HTTP kontrol seçenekleri</legend>
          <label className="sm:col-span-1">
            <span className="mb-1 block text-xs font-medium text-zinc-500">Metot</span>
            <select name="http_method" defaultValue={monitor.http.method} className={inputClass}>
              <option value="GET">GET</option>
              <option value="HEAD">HEAD</option>
            </select>
          </label>
          <label className="sm:col-span-2">
            <span className="mb-1 block text-xs font-medium text-zinc-500">
              Başarılı sayılan durum kodları
            </span>
            <input
              name="http_expected_status"
              required
              defaultValue={monitor.http.expected_status}
              placeholder="200-399"
              className={inputClass}
            />
          </label>
          <label className="sm:col-span-3">
            <span className="mb-1 block text-xs font-medium text-zinc-500">
              Yanıtta aranacak metin (isteğe bağlı)
            </span>
            <input
              name="http_keyword"
              defaultValue={monitor.http.keyword}
              placeholder={'"status":"ok"'}
              className={inputClass}
            />
          </label>
          <label className="sm:col-span-6">
            <span className="mb-1 block text-xs font-medium text-zinc-500">
              İstek header&apos;ları (satır başına bir tane, &quot;Ad: değer&quot;)
            </span>
            <textarea
              name="http_headers"
              rows={Math.max(2, Object.keys(monitor.http.headers).length + 1)}
              defaultValue={Object.entries(monitor.http.headers)
                .map(([name, value]) => `${name}: ${value}`)
                .join("\n")}
              placeholder="Authorization: Bearer ..."
              className={`${inputClass} font-mono`}
            />
            <span className="mt-1 block text-xs text-zinc-500">
              Kayıtlı değerler {MASKED_VALUE} olarak gösterilir; satırı öyle bırakırsanız değer
              korunur.
            </span>
          </label>
          <label className="flex items-center gap-2 text-sm sm:col-span-6">
            <input
              type="checkbox"
              name="tls_skip_verify"
              defaultChecked={monitor.http.tls_skip_verify}
            />
            TLS sertifikasını doğrulama (özel CA kullanan hedefler için)
          </label>
        </fieldset>
      )}
      <div className="flex items-end gap-3 sm:col-span-2">
        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-60"
        >
          {pending ? "Kaydediliyor…" : "Kaydet"}
        </button>
        {state && !pending && (
          <span
            role="status"
            className={`text-sm ${
              state.error
                ? "text-red-600 dark:text-red-400"
                : "text-emerald-700 dark:text-emerald-400"
            }`}
          >
            {state.error ?? "Kaydedildi"}
          </span>
        )}
      </div>
    </form>
  );
}
