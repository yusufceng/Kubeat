"use client";

import { useActionState, useState, useTransition } from "react";
import {
  type ChannelTestState,
  deleteChannelAction,
  saveChannelAction,
  setChannelEnabledAction,
  testChannelAction,
} from "@/app/actions";
import type { Channel, ChannelType } from "@/lib/api";
import { type ChannelEvent, channelEventLabels, channelTypeOrder, channelTypes } from "@/lib/channel-types";
import { MASKED_VALUE } from "@/lib/constants";
import { useT } from "@/lib/i18n/client";
import { EditIcon, PauseIcon, PlayIcon, TrashIcon } from "@/app/ui/icons";

/**
 * The form of a channel. With a channel it edits that one; without, it
 * creates one of the type chosen at the top.
 */
export function ChannelForm({
  channel,
  namespaces,
  onDone,
}: {
  channel?: Channel;
  /** The namespaces a channel can be limited to. */
  namespaces: string[];
  /** Called after a successful save. */
  onDone?: () => void;
}) {
  const t = useT();
  const [type, setType] = useState<ChannelType>(channel?.type ?? "slack");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const spec = channelTypes[type];
  // A namespace the channel is limited to may no longer have monitors.
  const offered = [...new Set([...namespaces, ...(channel?.namespaces ?? [])])].sort();

  // Submitted by hand: React resets a form after its action, which would
  // throw away what was typed into a rejected one.
  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    setError(null);
    startTransition(async () => {
      const result = await saveChannelAction(data);
      if (result?.error) {
        setError(result.error);
        return;
      }
      if (!channel) form.reset();
      onDone?.();
    });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 sm:grid-cols-6">
      {channel && <input type="hidden" name="id" value={channel.id} />}
      <input type="hidden" name="type" value={type} />
      <label className="sm:col-span-2">
        <span className="mb-1 block text-xs font-medium text-muted">{t("channelForm.type.label")}</span>
        {channel ? (
          <input defaultValue={t(spec.label)} disabled className="field" />
        ) : (
          <select value={type} onChange={(e) => setType(e.target.value as ChannelType)} className="field">
            {channelTypeOrder.map((kind) => (
              <option key={kind} value={kind}>
                {t(channelTypes[kind].label)}
              </option>
            ))}
          </select>
        )}
      </label>
      <label className="sm:col-span-4">
        <span className="mb-1 block text-xs font-medium text-muted">{t("channelForm.name.label")}</span>
        <input name="name" required defaultValue={channel?.name} placeholder={t("channelForm.name.placeholder")} className="field" />
      </label>
      <p className="-mt-1 text-xs text-muted sm:col-span-6">{t(spec.summary)}</p>

      {spec.target && (
        <label className={spec.secret ? "sm:col-span-3" : "sm:col-span-6"}>
          <span className="mb-1 block text-xs font-medium text-muted">{t(spec.target.label)}</span>
          <input
            // Remounted per type so a URL typed for one type is not checked
            // as something else for another.
            key={type}
            name="target"
            // A stored URL is shown masked, which is not a URL.
            type={channel ? "text" : spec.target.inputType}
            required
            defaultValue={channel?.target}
            placeholder={t(spec.target.placeholder)}
            className="field font-mono"
          />
          <span className="mt-1 block text-xs text-muted">
            {channel && channel.target.endsWith("…")
              ? t("channelForm.target.masked")
              : spec.target.hint && t(spec.target.hint)}
          </span>
        </label>
      )}
      {spec.secret && (
        <label className={spec.target ? "sm:col-span-3" : "sm:col-span-6"}>
          <span className="mb-1 block text-xs font-medium text-muted">{t(spec.secret.label)}</span>
          <input
            key={type}
            name="secret"
            type="password"
            required={spec.secret.required}
            // The stored value never reaches the browser; the mask stands for it.
            defaultValue={channel?.has_secret ? MASKED_VALUE : ""}
            placeholder={t(spec.secret.placeholder)}
            autoComplete="new-password"
            className="field font-mono"
          />
          {channel?.has_secret && (
            <span className="mt-1 block text-xs text-muted">{t("channelForm.secret.masked")}</span>
          )}
        </label>
      )}

      <fieldset className="sm:col-span-6">
        <legend className="mb-1 text-xs font-medium text-muted">{t("channelForm.scope.legend")}</legend>
        {offered.length === 0 ? (
          <p className="text-sm text-muted">{t("channelForm.scope.allMonitors")}</p>
        ) : (
          <>
            <div className="flex flex-wrap gap-x-4 gap-y-1.5">
              {offered.map((ns) => (
                <label key={ns} className="flex items-center gap-1.5 text-sm">
                  <input
                    type="checkbox"
                    name="namespace"
                    value={ns}
                    defaultChecked={channel?.namespaces.includes(ns)}
                    className="h-4 w-4 accent-(--accent)"
                  />
                  {ns}
                </label>
              ))}
            </div>
            <p className="mt-1 text-xs text-muted">{t("channelForm.scope.hint")}</p>
          </>
        )}
      </fieldset>

      <label className="sm:col-span-3">
        <span className="mb-1 block text-xs font-medium text-muted">{t("channelForm.tags.label")}</span>
        <input
          name="tags"
          defaultValue={channel?.tags.join(", ")}
          placeholder={t("channelForm.tags.placeholder")}
          className="field"
        />
        <span className="mt-1 block text-xs text-muted">{t("channelForm.tags.hint")}</span>
      </label>
      <fieldset className="sm:col-span-3">
        <legend className="mb-1 text-xs font-medium text-muted">{t("channelForm.events.legend")}</legend>
        <div className="flex flex-wrap gap-x-4 gap-y-1.5">
          {(Object.keys(channelEventLabels) as ChannelEvent[]).map((event) => (
            <label key={event} className="flex items-center gap-1.5 text-sm">
              <input
                type="checkbox"
                name="event"
                value={event}
                defaultChecked={!channel || channel.events.length === 0 || channel.events.includes(event)}
                className="h-4 w-4 accent-(--accent)"
              />
              {t(channelEventLabels[event])}
            </label>
          ))}
        </div>
        <span className="mt-1 block text-xs text-muted">{t("channelForm.events.hint")}</span>
      </fieldset>

      <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? t("common.saving") : channel ? t("common.save") : t("channelForm.create")}
        </button>
        {channel && (
          <button type="button" onClick={onDone} className="btn">
            {t("common.cancel")}
          </button>
        )}
        {error && (
          <p role="alert" className="text-sm text-down">
            {error}
          </p>
        )}
      </div>
    </form>
  );
}

/** One channel in the list: what it is, and what can be done with it. */
export function ChannelRow({ channel, namespaces }: { channel: Channel; namespaces: string[] }) {
  const t = useT();
  const [editing, setEditing] = useState(false);
  const [test, testAction, testing] = useActionState<ChannelTestState, FormData>(testChannelAction, null);
  const spec = channelTypes[channel.type];

  return (
    <li className="px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
            <span className={channel.enabled ? "" : "text-muted line-through"}>{channel.name}</span>
            <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal">{t(spec?.label ?? channel.type)}</span>
            {!channel.enabled && (
              <span className="rounded bg-sunken px-1.5 py-0.5 text-xs font-normal text-muted">{t("channelRow.paused")}</span>
            )}
          </div>
          <div className="mt-0.5 truncate text-xs text-muted">
            {channel.target && <span className="font-mono">{channel.target} · </span>}
            {channel.namespaces.length === 0
              ? t("channelRow.allMonitors")
              : t("channelRow.onlyNamespaces", { namespaces: channel.namespaces.join(", ") })}
            {channel.tags.length > 0 && ` · ${t("channelRow.tags", { tags: channel.tags.join(", ") })}`}
            {channel.events.length > 0 && ` · ${channel.events.map((e) => t(channelEventLabels[e]).toLowerCase()).join(", ")}`}
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <form action={testAction}>
            <input type="hidden" name="id" value={channel.id} />
            <button type="submit" disabled={testing} className="btn">
              {testing ? t("channelRow.test.sending") : t("channelRow.test.send")}
            </button>
          </form>
          <form action={setChannelEnabledAction}>
            <input type="hidden" name="id" value={channel.id} />
            <input type="hidden" name="enabled" value={String(!channel.enabled)} />
            <button type="submit" className="btn" title={channel.enabled ? t("channelRow.pause.title") : t("channelRow.resume.title")}>
              {channel.enabled ? <PauseIcon /> : <PlayIcon />}
              <span className="sr-only">{channel.enabled ? t("channelRow.pause") : t("channelRow.resume")}</span>
            </button>
          </form>
          <button type="button" onClick={() => setEditing((v) => !v)} aria-expanded={editing} className="btn" title={t("common.edit")}>
            <EditIcon />
            <span className="sr-only">{t("common.edit")}</span>
          </button>
          <form
            action={deleteChannelAction}
            onSubmit={(e) => {
              if (!confirm(t("channelRow.delete.confirm", { name: channel.name }))) e.preventDefault();
            }}
          >
            <input type="hidden" name="id" value={channel.id} />
            <button type="submit" className="btn btn-danger" title={t("common.delete")}>
              <TrashIcon />
              <span className="sr-only">{t("common.delete")}</span>
            </button>
          </form>
        </div>
      </div>
      {test && !testing && (
        <p role="status" className={`mt-2 text-xs ${test.delivered ? "text-up" : "text-down"}`}>
          {test.delivered ? t("channelRow.test.delivered") : t("channelRow.test.failed", { error: test.error ?? "" })}
        </p>
      )}
      {editing && (
        <div className="mt-3 rounded-lg bg-sunken/60 p-4">
          <ChannelForm channel={channel} namespaces={namespaces} onDone={() => setEditing(false)} />
        </div>
      )}
    </li>
  );
}
