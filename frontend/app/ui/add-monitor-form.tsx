"use client";

import { useActionState, useState } from "react";
import { createMonitorAction, type FormState } from "@/app/actions";

const initialState: FormState = { error: null, values: null };

const inputClass =
  "w-full rounded-md border border-black/15 bg-transparent px-3 py-2 text-sm outline-none focus:border-emerald-600 dark:border-white/20";

export function AddMonitorForm() {
  const [state, action, pending] = useActionState(createMonitorAction, initialState);
  const [type, setType] = useState("http");
  // React resets the form after every submit; a rejected one is refilled
  // from the values the action echoes back.
  const values = state.values;

  return (
    <form action={action} className="grid gap-4 sm:grid-cols-6">
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-zinc-500">İsim</span>
        <input
          name="name"
          required
          defaultValue={values?.name ?? ""}
          placeholder="odeme-api"
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-1">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Tip</span>
        <select
          name="type"
          value={type}
          onChange={(e) => setType(e.target.value)}
          className={inputClass}
        >
          <option value="http">HTTP</option>
          <option value="tcp">TCP</option>
        </select>
      </label>
      <label className="sm:col-span-3">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Hedef</span>
        <input
          name="target"
          required
          defaultValue={values?.target ?? ""}
          placeholder={type === "http" ? "https://ornek.com/healthz" : "veritabani.ic:5432"}
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Kontrol aralığı (sn)</span>
        <input
          name="interval_seconds"
          type="number"
          min={1}
          defaultValue={values?.interval_seconds ?? 30}
          required
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-1">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Zaman aşımı (sn)</span>
        <input
          name="timeout_seconds"
          type="number"
          min={1}
          defaultValue={values?.timeout_seconds ?? 5}
          required
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-1" title="Art arda bu kadar başarısız kontrolden sonra olay açılır">
        <span className="mb-1 block text-xs font-medium text-zinc-500">Alarm eşiği</span>
        <input
          name="failure_threshold"
          type="number"
          min={1}
          defaultValue={values?.failure_threshold ?? 3}
          required
          className={inputClass}
        />
      </label>
      <div className="flex items-end sm:col-span-2">
        <button
          type="submit"
          disabled={pending}
          className="w-full rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-60"
        >
          {pending ? "Ekleniyor…" : "Monitor ekle"}
        </button>
      </div>
      {state.error && (
        <p role="alert" className="text-sm text-red-600 sm:col-span-6 dark:text-red-400">
          {state.error}
        </p>
      )}
    </form>
  );
}
