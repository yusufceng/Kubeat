"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { cloneMonitorAction, deleteMonitorAction, setMonitorEnabledAction } from "@/app/actions";
import type { Monitor } from "@/lib/api";
import { useT } from "@/lib/i18n/client";
import { CopyIcon, EditIcon, PauseIcon, PlayIcon, TrashIcon } from "@/app/ui/icons";

/** What can be done with a monitor: pause or resume, edit, copy, delete. */
export function MonitorActions({ monitor }: { monitor: Pick<Monitor, "id" | "name" | "enabled" | "managed"> }) {
  const t = useT();
  const [error, setError] = useState<string | null>(null);
  const [cloning, startClone] = useTransition();
  // The annotation's name is markup, so the hint is split around it.
  const managedHint = t("monitorPage.actions.managedHint").split("kubeat.io/monitor");

  function clone() {
    const name = prompt(
      t("monitorPage.actions.cloneName"),
      t("monitorPage.actions.cloneDefault", { name: monitor.name }),
    )?.trim();
    if (!name) return;
    setError(null);
    startClone(async () => {
      const result = await cloneMonitorAction(monitor.id, name);
      if (result?.error) setError(result.error); // on success the action redirects
    });
  }

  return (
    <div className="flex flex-col items-end gap-2">
      <div className="flex flex-wrap items-center justify-end gap-2">
        <form action={setMonitorEnabledAction}>
          <input type="hidden" name="id" value={monitor.id} />
          <input type="hidden" name="enabled" value={String(!monitor.enabled)} />
          <button type="submit" className="btn">
            {monitor.enabled ? (
              <>
                <PauseIcon /> {t("monitorPage.actions.pause")}
              </>
            ) : (
              <>
                <PlayIcon /> {t("monitorPage.actions.resume")}
              </>
            )}
          </button>
        </form>
        <Link href={`/monitors/${monitor.id}/edit`} className="btn">
          <EditIcon /> {t("common.edit")}
        </Link>
        <button type="button" onClick={clone} disabled={cloning} className="btn">
          <CopyIcon /> {cloning ? t("monitorPage.actions.copying") : t("monitorPage.copy")}
        </button>
        {!monitor.managed && (
          <form
            action={deleteMonitorAction}
            onSubmit={(e) => {
              if (!confirm(t("monitorPage.actions.confirmDelete", { name: monitor.name }))) e.preventDefault();
            }}
          >
            <input type="hidden" name="id" value={monitor.id} />
            <button type="submit" className="btn btn-danger">
              <TrashIcon /> {t("common.delete")}
            </button>
          </form>
        )}
      </div>
      {monitor.managed && (
        <p className="max-w-xs text-right text-xs text-muted">
          {managedHint[0]}
          <code>kubeat.io/monitor</code>
          {managedHint[1]}
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-down">
          {error}
        </p>
      )}
    </div>
  );
}
