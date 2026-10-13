"use client";

import { ConfirmForm, PromptDialog } from "@/app/ui/confirm";
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
  const [naming, setNaming] = useState(false);

  function clone(name: string) {
    setNaming(false);
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
        <button type="button" onClick={() => setNaming(true)} disabled={cloning} className="btn">
          <CopyIcon /> {cloning ? t("monitorPage.actions.copying") : t("monitorPage.copy")}
        </button>
        {(
          <ConfirmForm
            action={deleteMonitorAction}
            question={
              t("monitorPage.actions.confirmDelete", { name: monitor.name }) +
              (monitor.managed ? " " + t("monitorPage.actions.confirmDelete.managed") : "")
            }
          >
            <input type="hidden" name="id" value={monitor.id} />
            <button type="submit" className="btn btn-danger">
              <TrashIcon /> {t("common.delete")}
            </button>
          </ConfirmForm>
        )}
      </div>
      {error && (
        <p role="alert" className="text-sm text-down">
          {error}
        </p>
      )}
    </div>
  );
}
