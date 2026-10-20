"use client";

import Link from "next/link";
import { useActionState, useEffect, useId, useRef, useState, useTransition } from "react";
import {
  cloneMonitorAction,
  createMaintenanceAction,
  deleteMonitorAction,
  type MaintenanceFormState,
  setMonitorEnabledAction,
} from "@/app/actions";
import type { Monitor } from "@/lib/api";
import { duration } from "@/lib/format";
import { useT } from "@/lib/i18n/client";
import { ConfirmDialog, PromptDialog } from "@/app/ui/confirm";
import { CopyIcon, EditIcon, MoreIcon, PauseIcon, PlayIcon, TrashIcon } from "@/app/ui/icons";
import { ChevronDownIcon, SilenceIcon } from "@/app/ui/monitor-icons";
import { Menu } from "@/app/ui/monitor-menu";

const silenced: MaintenanceFormState = { error: null, created: false };

/**
 * What can be done with a monitor, as quiet buttons: pause or resume,
 * silence its notifications for a while, edit; copy and delete wait in a
 * menu, since they are rarer and one of them cannot be undone. silence
 * lists the lengths offered, or is null where silencing makes no sense
 * (a paused monitor, or one already in maintenance).
 */
export function MonitorActions({
  monitor,
  silence,
}: {
  monitor: Pick<Monitor, "id" | "name" | "enabled" | "managed">;
  silence: number[] | null;
}) {
  const t = useT();
  const [error, setError] = useState<string | null>(null);
  const [cloning, startClone] = useTransition();
  const [naming, setNaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const deleteForm = useRef<HTMLFormElement>(null);

  function clone(name: string) {
    setNaming(false);
    setError(null);
    startClone(async () => {
      const result = await cloneMonitorAction(monitor.id, name);
      if (result?.error) setError(result.error); // on success the action redirects
    });
  }

  return (
    <div className="mon-acts" role="group" aria-label={t("monitorPage.actions.label")}>
      <form action={setMonitorEnabledAction}>
        <input type="hidden" name="id" value={monitor.id} />
        <input type="hidden" name="enabled" value={String(!monitor.enabled)} />
        <button type="submit" className="btn btn-ghost">
          {monitor.enabled ? <PauseIcon /> : <PlayIcon />}
          {t(monitor.enabled ? "monitorPage.actions.pause" : "monitorPage.actions.resume")}
        </button>
      </form>
      {silence && <SilenceMenu monitorId={monitor.id} durations={silence} onError={setError} />}
      <Link href={`/monitors/${monitor.id}/edit`} className="btn btn-ghost">
        <EditIcon />
        {t("common.edit")}
      </Link>
      <Menu
        label={t("monitorPage.actions.more")}
        buttonLabel={t("monitorPage.actions.more")}
        button={<MoreIcon />}
        buttonClassName="btn btn-ghost px-2"
        className="w-52"
      >
        <button type="button" role="menuitem" className="menu-item" disabled={cloning} onClick={() => setNaming(true)}>
          <CopyIcon />
          {cloning ? t("monitorPage.actions.copying") : t("monitorPage.copy")}
        </button>
        <hr className="menu-sep" />
        <button
          type="button"
          role="menuitem"
          className="menu-item text-down-ink [&>svg]:text-down-ink"
          onClick={() => setDeleting(true)}
        >
          <TrashIcon />
          {t("common.delete")}
        </button>
      </Menu>
      <form ref={deleteForm} action={deleteMonitorAction} hidden>
        <input type="hidden" name="id" value={monitor.id} />
      </form>
      {naming && (
        <PromptDialog
          title={t("monitorPage.actions.cloneTitle")}
          label={t("monitorPage.actions.cloneName")}
          initial={t("monitorPage.actions.cloneDefault", { name: monitor.name })}
          submitLabel={t("monitorPage.copy")}
          onSubmit={clone}
          onCancel={() => setNaming(false)}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title={t("monitorPage.actions.deleteTitle", { name: monitor.name })}
          confirmLabel={t("common.delete")}
          onCancel={() => setDeleting(false)}
          onConfirm={() => {
            setDeleting(false);
            deleteForm.current?.requestSubmit();
          }}
        >
          {t("monitorPage.actions.deleteBody")}
          {monitor.managed && <span className="mt-2 block">{t("monitorPage.actions.confirmDelete.managed")}</span>}
        </ConfirmDialog>
      )}
      {error && (
        <p role="alert" className="basis-full text-right text-sm text-down-ink">
          {error}
        </p>
      )}
    </div>
  );
}

/**
 * Silence: a maintenance window for this monitor alone, from now on, as
 * long as the item picked says. It takes the place of the select and the
 * button that used to float between the panels.
 */
function SilenceMenu({
  monitorId,
  durations,
  onError,
}: {
  monitorId: number;
  durations: number[];
  onError: (error: string | null) => void;
}) {
  const t = useT();
  const [state, action, pending] = useActionState(createMaintenanceAction, silenced);
  const hint = useId();

  useEffect(() => {
    if (!pending) onError(state.error);
  }, [state, pending, onError]);

  return (
    <Menu
      label={t("monitorPage.actions.silence.heading")}
      describedBy={hint}
      button={
        <>
          <SilenceIcon />
          {pending ? t("maintenance.silence.silencing") : t("monitorPage.actions.silence")}
          <ChevronDownIcon width={14} height={14} className="-mr-1 text-ink-3" />
        </>
      }
      className="w-64"
    >
      <form action={action}>
        <input type="hidden" name="name" value={t("maintenance.silence.name")} />
        <input type="hidden" name="scope" value={`mon:${monitorId}`} />
        {/* Seen above the items; screen readers have it as the menu's name and description. */}
        <p aria-hidden className="px-2.5 pt-1.5 pb-2">
          <span className="block text-[13px] font-semibold">{t("monitorPage.actions.silence.heading")}</span>
          <span id={hint} className="mt-0.5 block text-xs text-ink-3">
            {t("monitorPage.actions.silence.hint")}
          </span>
        </p>
        <hr className="menu-sep" />
        {durations.map((d) => (
          <button
            key={d}
            type="submit"
            name="duration_seconds"
            value={d}
            role="menuitem"
            disabled={pending}
            className="menu-item tabular-nums"
          >
            {t("monitorPage.actions.silence.for", { duration: duration(d, t) })}
          </button>
        ))}
      </form>
    </Menu>
  );
}
