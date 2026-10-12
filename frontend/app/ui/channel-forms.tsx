"use client";

import { useActionState, useState } from "react";
import {
  type ChannelFormState,
  type ChannelTestState,
  createChannelAction,
  deleteChannelAction,
  testChannelAction,
} from "@/app/actions";
import type { ChannelType } from "@/lib/api";
import { channelTypes } from "@/lib/channel-types";

const inputClass = "field";

const initialState: ChannelFormState = { error: null, values: null };


export function AddChannelForm() {
  const [state, action, pending] = useActionState(createChannelAction, initialState);
  const [type, setType] = useState<ChannelType>((state.values?.type as ChannelType) ?? "slack");
  const spec = channelTypes[type];

  return (
    <form action={action} className="grid gap-4 sm:grid-cols-6">
      <label className="sm:col-span-1">
        <span className="mb-1 block text-xs font-medium text-muted">Tür</span>
        <select
          name="type"
          value={type}
          onChange={(e) => setType(e.target.value as ChannelType)}
          className={inputClass}
        >
          {(Object.keys(channelTypes) as ChannelType[]).map((t) => (
            <option key={t} value={t}>
              {channelTypes[t].label}
            </option>
          ))}
        </select>
      </label>
      <label className="sm:col-span-1">
        <span className="mb-1 block text-xs font-medium text-muted">İsim</span>
        <input
          name="name"
          required
          defaultValue={state.values?.name ?? ""}
          placeholder="ops-kanali"
          className={inputClass}
        />
      </label>
      <label className="sm:col-span-3">
        <span className="mb-1 block text-xs font-medium text-muted">{spec.targetLabel}</span>
        <input
          // Remounted per type so a URL typed for one type is not validated
          // as an address list for another.
          key={type}
          name="target"
          type={spec.inputType}
          required
          defaultValue={state.values?.type === type ? state.values.target : ""}
          placeholder={spec.placeholder}
          className={inputClass}
        />
      </label>
      <div className="flex items-end sm:col-span-1">
        <button
          type="submit"
          disabled={pending}
          className="w-full btn btn-primary"
        >
          {pending ? "Ekleniyor…" : "Ekle"}
        </button>
      </div>
      {state.error && (
        <p role="alert" className="text-sm text-down sm:col-span-6">
          {state.error}
        </p>
      )}
    </form>
  );
}

/** Test and delete buttons of one channel, with the outcome of the last test. */
export function ChannelActions({ id, name }: { id: number; name: string }) {
  const [test, testAction, testing] = useActionState<ChannelTestState, FormData>(
    testChannelAction,
    null,
  );

  return (
    <div className="flex flex-wrap items-center justify-end gap-2">
      {test && !testing && (
        <span
          role="status"
          className={`text-xs ${
            test.delivered
              ? "text-up"
              : "text-down"
          }`}
        >
          {test.delivered ? "Test bildirimi gönderildi" : `Gönderilemedi: ${test.error}`}
        </span>
      )}
      <form action={testAction}>
        <input type="hidden" name="id" value={id} />
        <button
          type="submit"
          disabled={testing}
          className="btn"
        >
          {testing ? "Gönderiliyor…" : "Test gönder"}
        </button>
      </form>
      <form
        action={deleteChannelAction}
        onSubmit={(e) => {
          if (!confirm(`"${name}" kanalı silinsin mi?`)) e.preventDefault();
        }}
      >
        <input type="hidden" name="id" value={id} />
        <button
          type="submit"
          className="btn btn-danger"
        >
          Sil
        </button>
      </form>
    </div>
  );
}
