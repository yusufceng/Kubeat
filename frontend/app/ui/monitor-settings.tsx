"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { cloneMonitorAction, deleteMonitorAction, setMonitorEnabledAction } from "@/app/actions";
import type { Monitor } from "@/lib/api";
import { CopyIcon, EditIcon, PauseIcon, PlayIcon, TrashIcon } from "@/app/ui/icons";

/** What can be done with a monitor: pause or resume, edit, copy, delete. */
export function MonitorActions({ monitor }: { monitor: Pick<Monitor, "id" | "name" | "enabled" | "managed"> }) {
  const [error, setError] = useState<string | null>(null);
  const [cloning, startClone] = useTransition();

  function clone() {
    const name = prompt("Kopyanın adı", `${monitor.name} (kopya)`)?.trim();
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
                <PauseIcon /> Durdur
              </>
            ) : (
              <>
                <PlayIcon /> Devam ettir
              </>
            )}
          </button>
        </form>
        <Link href={`/monitors/${monitor.id}/edit`} className="btn">
          <EditIcon /> Düzenle
        </Link>
        <button type="button" onClick={clone} disabled={cloning} className="btn">
          <CopyIcon /> {cloning ? "Kopyalanıyor…" : "Kopyala"}
        </button>
        {!monitor.managed && (
          <form
            action={deleteMonitorAction}
            onSubmit={(e) => {
              if (!confirm(`"${monitor.name}" ve tüm kontrol geçmişi silinsin mi?`)) e.preventDefault();
            }}
          >
            <input type="hidden" name="id" value={monitor.id} />
            <button type="submit" className="btn btn-danger">
              <TrashIcon /> Sil
            </button>
          </form>
        )}
      </div>
      {monitor.managed && (
        <p className="max-w-xs text-right text-xs text-muted">
          Silmek için nesnedeki <code>kubeat.io/monitor</code> annotation&apos;ını kaldırın.
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
