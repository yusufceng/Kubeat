"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useActionState, useEffect, useState, useTransition } from "react";
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
import type { T } from "@/lib/i18n/translate";
import { ConfirmForm } from "@/app/ui/confirm";
import { EditIcon, PauseIcon, PlayIcon, TrashIcon } from "@/app/ui/icons";
import { CheckIcon, CloseIcon, SendIcon } from "@/app/ui/page-icons";
import { StatusGlyph } from "@/app/ui/status-glyph";
import { TableTime } from "@/app/ui/table-time";

const allEvents = Object.keys(channelEventLabels) as ChannelEvent[];

/** Words joined as a sentence would: "a, b ve c" / "a, b and c". */
function listWords(words: string[], t: T): string {
  return new Intl.ListFormat(t.intl, { type: "conjunction" }).format(words);
}

/** Tags as the form takes them: "pager, kritik" -> ["pager", "kritik"]. */
function tagList(text: string): string[] {
  return text
    .split(",")
    .map((tag) => tag.trim())
    .filter(Boolean);
}

/**
 * The sentence that reads a channel's routing back: which monitors' which
 * notifications reach it. No namespace ticked is every monitor, and no
 * event ticked (or all of them) is every event, as the backend has it.
 */
function routing(namespaces: string[], tags: string[], events: ChannelEvent[], t: T): React.ReactNode {
  const ns = listWords(namespaces, t);
  // A monitor needs one of the tags, not all of them.
  const tg = new Intl.ListFormat(t.intl, { type: "disjunction" }).format(tags);
  const who =
    namespaces.length > 0 && tags.length > 0
      ? t.n("channelForm.echo.who.nsTags", namespaces.length, { namespaces: ns, tags: tg })
      : namespaces.length > 0
        ? t.n("channelForm.echo.who.ns", namespaces.length, { namespaces: ns })
        : tags.length > 0
          ? t("channelForm.echo.who.tags", { tags: tg })
          : t("channelForm.echo.who.all");
  const what =
    events.length === 0 || events.length === allEvents.length
      ? t("channelForm.echo.what.all")
      : t("channelForm.echo.what.some", {
          events: listWords(
            events.map((e) => t(channelEventLabels[e]).toLocaleLowerCase(t.intl)),
            t,
          ),
        });
  const [before, middle] = t("channelForm.echo").split("{who}");
  if (middle !== undefined && before.includes("{what}")) {
    // English: "This channel gets {what} of {who}."
    const [lead, rest] = before.split("{what}");
    return (
      <>
        {lead}
        <b>{what}</b>
        {rest}
        <b>{who}</b>
        {middle}
      </>
    );
  }
  // Turkish: "Bu kanala {who} {what} gider."
  const [between, after] = (middle ?? "").split("{what}");
  return (
    <>
      {before}
      <b>{who}</b>
      {between}
      <b>{what}</b>
      {after}
    </>
  );
}

/**
 * The form of a channel. With a channel it edits that one; without, it
 * creates one of the type picked at the top. Its sections are what the
 * channel is, where it sends, and what it hears about, the last read back
 * as a sentence. closeHref is where Cancel and a successful save lead.
 */
export function ChannelForm({
  channel,
  namespaces,
  closeHref,
}: {
  channel?: Channel;
  /** The namespaces a channel can be limited to. */
  namespaces: string[];
  closeHref: string;
}) {
  const t = useT();
  const router = useRouter();
  const [type, setType] = useState<ChannelType>(channel?.type ?? "slack");
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  // What the routing sentence reads, kept as the boxes are ticked.
  const [chosenNamespaces, setChosenNamespaces] = useState<string[]>(channel?.namespaces ?? []);
  const [tags, setTags] = useState(channel?.tags.join(", ") ?? "");
  const [events, setEvents] = useState<ChannelEvent[]>(
    !channel || channel.events.length === 0 ? allEvents : channel.events,
  );
  const spec = channelTypes[type];
  // A namespace the channel is limited to may no longer have monitors.
  const offered = [...new Set([...namespaces, ...(channel?.namespaces ?? [])])].sort();
  const toggle = <V,>(list: V[], value: V) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);

  // Submitted by hand: React resets a form after its action, which would
  // throw away what was typed into a rejected one.
  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    setError(null);
    startTransition(async () => {
      const result = await saveChannelAction(data);
      if (result?.error) {
        setError(result.error);
        return;
      }
      router.replace(closeHref, { scroll: false });
    });
  }

  return (
    <form onSubmit={submit} className="pg-form">
      {channel && <input type="hidden" name="id" value={channel.id} />}
      <input type="hidden" name="type" value={type} />

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("channelForm.sec.channel")}</h2>
          <p>{t("channelForm.sec.channelBody")}</p>
        </div>
        <div className="pg-fields">
          {channel ? (
            <div>
              <span className="pg-label">{t("channelForm.type.label")}</span>
              <span className="pg-tag">{t(spec.label)}</span>
            </div>
          ) : (
            <fieldset>
              <legend className="pg-label">{t("channelForm.type.label")}</legend>
              <div className="pg-types">
                {channelTypeOrder.map((kind) => (
                  <label key={kind} className="pg-type">
                    <input type="radio" name="channel-type" checked={type === kind} onChange={() => setType(kind)} />
                    {t(channelTypes[kind].label)}
                  </label>
                ))}
              </div>
            </fieldset>
          )}
          <label>
            <span className="pg-label">{t("channelForm.name.label")}</span>
            <input
              name="name"
              required
              defaultValue={channel?.name}
              placeholder={t("channelForm.name.placeholder")}
              className="field"
            />
          </label>
        </div>
      </div>

      {(spec.target || spec.secret) && (
        <div className="pg-sec">
          <div className="pg-sec-intro">
            <h2>{t("channelForm.sec.target")}</h2>
            <p>{t(spec.summary)}</p>
          </div>
          <div className="pg-fields">
            {spec.target && (
              <label>
                <span className="pg-label">{t(spec.target.label)}</span>
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
                {(channel?.target.endsWith("…") || spec.target.hint) && (
                  <span className="pg-help">
                    {channel?.target.endsWith("…") ? t("channelForm.target.masked") : spec.target.hint && t(spec.target.hint)}
                  </span>
                )}
              </label>
            )}
            {spec.secret && (
              <label>
                <span className="pg-label">{t(spec.secret.label)}</span>
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
                {channel?.has_secret && <span className="pg-help">{t("channelForm.secret.masked")}</span>}
              </label>
            )}
          </div>
        </div>
      )}

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("channelForm.sec.routing")}</h2>
          <p>{t("channelForm.sec.routingBody")}</p>
        </div>
        <div className="pg-fields">
          <fieldset>
            <legend className="pg-label">{t("channelForm.scope.legend")}</legend>
            {offered.length === 0 ? (
              <p className="pg-help mt-0">{t("channelForm.scope.allMonitors")}</p>
            ) : (
              <>
                <div className="pg-chips">
                  {offered.map((ns) => (
                    <label key={ns} className="pg-chip pg-chip-mono">
                      <input
                        type="checkbox"
                        name="namespace"
                        value={ns}
                        checked={chosenNamespaces.includes(ns)}
                        onChange={() => setChosenNamespaces((list) => toggle(list, ns))}
                      />
                      {ns}
                    </label>
                  ))}
                </div>
                <span className="pg-help">{t("channelForm.scope.hint")}</span>
              </>
            )}
          </fieldset>
          <label>
            <span className="pg-label">
              {t("channelForm.tags.label")}
              <small>{t("form.optional")}</small>
            </span>
            <input
              name="tags"
              value={tags}
              onChange={(e) => setTags(e.target.value)}
              placeholder={t("channelForm.tags.placeholder")}
              className="field"
            />
            <span className="pg-help">{t("channelForm.tags.hint")}</span>
          </label>
          <fieldset>
            <legend className="pg-label">{t("channelForm.events.legend")}</legend>
            <div className="pg-chips">
              {allEvents.map((event) => (
                <label key={event} className="pg-chip">
                  <input
                    type="checkbox"
                    name="event"
                    value={event}
                    checked={events.includes(event)}
                    onChange={() => setEvents((list) => toggle(list, event))}
                  />
                  {t(channelEventLabels[event])}
                </label>
              ))}
            </div>
            <span className="pg-help">{t("channelForm.events.hint")}</span>
          </fieldset>
          <p className="pg-echo" aria-live="polite">
            {routing(chosenNamespaces, tagList(tags), events, t)}
          </p>
        </div>
      </div>

      <div className="pg-savebar pg-savebar-sticky">
        {error && <p role="alert">{error}</p>}
        <div className="pg-savebar-end">
          <Link href={closeHref} scroll={false} className="btn btn-ghost">
            {t("common.cancel")}
          </Link>
          <button type="submit" disabled={pending} className="btn btn-primary">
            {pending ? t("common.saving") : channel ? t("common.save") : t("channelForm.create")}
          </button>
        </div>
      </div>
    </form>
  );
}

/**
 * Sends a test notification through a channel. The channel keeps the
 * attempt as its last delivery, so the row is read again afterwards; the
 * button says for a moment how it went.
 */
function TestButton({ channel }: { channel: Channel }) {
  const t = useT();
  const router = useRouter();
  const [result, action, testing] = useActionState<ChannelTestState, FormData>(testChannelAction, null);
  // The outcome is shown until a few seconds have passed since it came.
  const [faded, setFaded] = useState<ChannelTestState>(null);
  const shown = result && result !== faded ? result : null;

  useEffect(() => {
    if (!result) return;
    router.refresh();
    const id = setTimeout(() => setFaded(result), 4000);
    return () => clearTimeout(id);
  }, [result, router]);

  const said = shown
    ? shown.delivered
      ? t("channelRow.test.delivered")
      : t("channelRow.test.failed", { error: shown.error ?? "" })
    : "";
  return (
    <form action={action} className="contents">
      <input type="hidden" name="id" value={channel.id} />
      <button
        type="submit"
        disabled={testing}
        className={`pg-icon-btn ${shown ? (shown.delivered ? "text-up-ink" : "text-down-ink") : ""}`}
        aria-label={`${t("channelRow.test.send")}: ${channel.name}`}
        title={said || (testing ? t("channelRow.test.sending") : t("channelRow.test.send"))}
      >
        {shown ? shown.delivered ? <CheckIcon /> : <CloseIcon /> : <SendIcon />}
      </button>
      <span role="status" className="sr-only">
        {said}
      </span>
    </form>
  );
}

/** Which monitors and which events a channel hears about, in one short line. */
function routingLine(channel: Channel, t: T): string {
  const parts = [
    channel.namespaces.length === 0 ? t("channelRow.allMonitors") : channel.namespaces.map((ns) => `ns/${ns}`).join(", "),
  ];
  if (channel.tags.length > 0) parts.push(t("channelRow.tags", { tags: channel.tags.join(", ") }));
  parts.push(
    channel.events.length === 0
      ? t("channelRow.allEvents")
      : listWords(
          channel.events.map((e) => t(channelEventLabels[e]).toLocaleLowerCase(t.intl)),
          t,
        ),
  );
  return parts.join(" · ");
}

/**
 * One channel as a row of the table: its name and type, where it sends,
 * what it hears about, how its last delivery went, and what can be done
 * with it. While it is edited (editHref leads back out), its form opens
 * in a row of its own under it.
 */
export function ChannelRow({
  channel,
  namespaces,
  canEdit,
  editing,
}: {
  channel: Channel;
  namespaces: string[];
  canEdit: boolean;
  editing: boolean;
}) {
  const t = useT();
  const spec = channelTypes[channel.type];
  const typeName = t(spec?.label ?? channel.type);
  const last = channel.last_delivery;
  const routes = routingLine(channel, t);

  return (
    <>
      <tr className={editing ? "pg-row-open" : undefined}>
        <td className="pg-grow">
          <span className="flex min-w-0 items-center gap-x-2 gap-y-1 max-sm:flex-wrap">
            <span className={`pg-trunc pg-name ${channel.enabled ? "" : "text-ink-3"}`} title={channel.name}>
              {channel.name}
            </span>
            <span className="pg-tag flex-none">{typeName}</span>
            {!channel.enabled && (
              <span className="pg-state flex-none text-ink-3">
                <StatusGlyph kind="paused" />
                {t("channelRow.paused")}
              </span>
            )}
          </span>
        </td>
        {/* Between a phone and a wide screen, the masked address gives way to the name. */}
        <td className="pg-shrink sm:max-xl:hidden">
          <span className="pg-trunc pg-mono max-w-60 text-ink-2" title={channel.target || undefined}>
            {channel.target || t("common.none")}
          </span>
        </td>
        <td className="pg-shrink">
          <span className="pg-trunc max-w-48 text-ink-2 xl:max-w-72" title={routes}>
            {routes}
          </span>
        </td>
        <td className="pg-shrink">
          {last ? (
            <span
              className={`pg-state max-w-64 ${last.delivered ? "text-ink-2" : "text-down-ink"}`}
              title={last.delivered ? undefined : last.error}
            >
              <StatusGlyph kind={last.delivered ? "up" : "down"} />
              <span className="tabular-nums">
                <TableTime iso={last.at} />
              </span>
              {!last.delivered && (
                <span className="pg-trunc pg-mono font-normal">{last.error || t("channelRow.failed")}</span>
              )}
            </span>
          ) : (
            <span className="text-ink-3">{t("channelRow.never")}</span>
          )}
        </td>
        {canEdit && (
          <td className="pg-actions">
            <span className="pg-row-actions">
              <TestButton channel={channel} />
              <form action={setChannelEnabledAction} className="contents">
                <input type="hidden" name="id" value={channel.id} />
                <input type="hidden" name="enabled" value={String(!channel.enabled)} />
                <button
                  type="submit"
                  className="pg-icon-btn"
                  aria-label={`${channel.enabled ? t("channelRow.pause") : t("channelRow.resume")}: ${channel.name}`}
                  title={channel.enabled ? t("channelRow.pause.title") : t("channelRow.resume.title")}
                >
                  {channel.enabled ? <PauseIcon /> : <PlayIcon />}
                </button>
              </form>
              <Link
                href={editing ? "/channels" : `/channels?edit=${channel.id}`}
                scroll={false}
                className="pg-icon-btn"
                aria-label={`${t("common.edit")}: ${channel.name}`}
                aria-expanded={editing}
                title={t("common.edit")}
              >
                <EditIcon />
              </Link>
              <ConfirmForm
                action={deleteChannelAction}
                className="contents"
                title={t("channelRow.delete.title", { name: channel.name })}
                question={t("channelRow.delete.body")}
                items={[`${channel.name} · ${typeName}${channel.target ? ` · ${channel.target}` : ""}`]}
                icon={<TrashIcon />}
              >
                <input type="hidden" name="id" value={channel.id} />
                <button
                  type="submit"
                  className="pg-icon-btn pg-icon-btn-danger"
                  aria-label={`${t("common.delete")}: ${channel.name}`}
                  title={t("common.delete")}
                >
                  <TrashIcon />
                </button>
              </ConfirmForm>
            </span>
          </td>
        )}
      </tr>
      {editing && (
        <tr className="pg-row-form">
          <td colSpan={canEdit ? 5 : 4}>
            <div className="pg-inline-form">
              <ChannelForm channel={channel} namespaces={namespaces} closeHref="/channels" />
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
