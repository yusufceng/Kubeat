"use client";

import Link from "next/link";
import { useState, useTransition } from "react";
import { deleteStatusPageAction, saveStatusPageAction } from "@/app/actions";
import type { StatusPage } from "@/lib/api";
import { MANUAL_LABEL } from "@/lib/namespaces";
import { EditIcon, TrashIcon } from "@/app/ui/icons";

/** A monitor a status page can show. */
export type PageMonitorOption = { id: number; label: string; namespace: string };

/**
 * The form of a status page. With a page it edits that one; without, it
 * creates one.
 */
export function StatusPageForm({
  page,
  monitors,
  onDone,
}: {
  page?: StatusPage;
  monitors: PageMonitorOption[];
  /** Called after a successful save. */
  onDone?: () => void;
}) {
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const chosen = new Map(page?.monitors.map((m) => [m.monitor_id, m.display_name]));

  const groups = new Map<string, PageMonitorOption[]>();
  for (const m of monitors) groups.set(m.namespace, [...(groups.get(m.namespace) ?? []), m]);

  // Submitted by hand: React resets a form after its action, which would
  // throw away what was typed into a rejected one.
  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = e.currentTarget;
    const data = new FormData(form);
    setError(null);
    startTransition(async () => {
      const result = await saveStatusPageAction(data);
      if (result?.error) {
        setError(result.error);
        return;
      }
      if (!page) form.reset();
      onDone?.();
    });
  }

  return (
    <form onSubmit={submit} className="grid gap-4 sm:grid-cols-6">
      {page && <input type="hidden" name="id" value={page.id} />}
      <label className="sm:col-span-3">
        <span className="mb-1 block text-xs font-medium text-muted">Başlık</span>
        <input name="title" required maxLength={100} defaultValue={page?.title} placeholder="Mağaza servisleri" className="field" />
      </label>
      <label className="sm:col-span-3">
        <span className="mb-1 block text-xs font-medium text-muted">Adres</span>
        <span className="flex items-center gap-1.5">
          <span className="font-mono text-sm text-muted">/status/</span>
          <input
            name="slug"
            required
            pattern="[a-z0-9]([a-z0-9\-]*[a-z0-9])?"
            title="Küçük harf, rakam ve tire"
            defaultValue={page?.slug}
            placeholder="magaza"
            className="field font-mono"
          />
        </span>
      </label>
      <label className="sm:col-span-6">
        <span className="mb-1 block text-xs font-medium text-muted">Açıklama (isteğe bağlı)</span>
        <input name="description" defaultValue={page?.description} placeholder="Müşterilerin kullandığı servislerin durumu" className="field" />
      </label>
      <label className="sm:col-span-6">
        <span className="mb-1 block text-xs font-medium text-muted">Duyuru (isteğe bağlı)</span>
        <textarea
          name="announcement"
          rows={2}
          defaultValue={page?.announcement}
          placeholder="Cumartesi 02:00-04:00 arasında planlı bakım yapılacaktır."
          className="field"
        />
        <span className="mt-1 block text-xs text-muted">Sayfanın en üstünde gösterilir; kaldırmak için boşaltın.</span>
      </label>

      <fieldset className="sm:col-span-6">
        <legend className="mb-1 text-xs font-medium text-muted">Gösterilecek monitor&apos;lar</legend>
        {monitors.length === 0 ? (
          <p className="text-sm text-muted">Henüz monitor yok.</p>
        ) : (
          <div className="max-h-80 overflow-y-auto rounded-lg border border-line bg-panel">
            {[...groups.entries()].map(([namespace, group]) => (
              <div key={namespace}>
                <div className="sticky top-0 border-b border-line bg-sunken px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-muted">
                  {namespace || MANUAL_LABEL}
                </div>
                {group.map((m) => (
                  <div key={m.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-line/60 px-3 py-2 last:border-b-0">
                    <label className="flex min-w-0 flex-1 items-center gap-2 text-sm">
                      <input
                        type="checkbox"
                        name="monitor"
                        value={m.id}
                        defaultChecked={chosen.has(m.id)}
                        className="h-4 w-4 shrink-0 accent-(--accent)"
                      />
                      <span className="truncate">{m.label}</span>
                    </label>
                    <input
                      name={`name_${m.id}`}
                      defaultValue={chosen.get(m.id)}
                      maxLength={100}
                      aria-label={`${m.label} için sayfada görünen ad`}
                      placeholder="Sayfada görünen ad (isteğe bağlı)"
                      className="field w-full py-1 text-xs sm:w-64"
                    />
                  </div>
                ))}
              </div>
            ))}
          </div>
        )}
        <p className="mt-1 text-xs text-muted">
          Sayfa yalnızca adı, durumu ve uptime geçmişini gösterir; adresler ve hata ayrıntıları gösterilmez.
        </p>
      </fieldset>

      <label className="flex items-center gap-2 text-sm sm:col-span-6">
        <input type="checkbox" name="published" defaultChecked={page?.published ?? true} className="h-4 w-4 accent-(--accent)" />
        Yayında (işaretli değilse sayfa açılmaz)
      </label>

      <div className="flex flex-wrap items-center gap-3 sm:col-span-6">
        <button type="submit" disabled={pending} className="btn btn-primary">
          {pending ? "Kaydediliyor…" : page ? "Kaydet" : "Sayfayı oluştur"}
        </button>
        {page && (
          <button type="button" onClick={onDone} className="btn">
            Vazgeç
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

/** One status page in the list. */
export function StatusPageRow({ page, monitors }: { page: StatusPage; monitors: PageMonitorOption[] }) {
  const [editing, setEditing] = useState(false);
  const labels = new Map(monitors.map((m) => [m.id, m.label]));

  return (
    <li className="px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
            {page.title}
            <span className={`rounded px-1.5 py-0.5 text-xs font-normal ${page.published ? "bg-up/12 text-up" : "bg-sunken text-muted"}`}>
              {page.published ? "Yayında" : "Taslak"}
            </span>
          </div>
          <div className="mt-0.5 text-xs text-muted">
            <span className="font-mono">/status/{page.slug}</span> · {page.monitors.length} monitor
            {page.announcement && " · duyuru var"}
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {page.published && (
            <Link href={`/status/${page.slug}`} target="_blank" className="btn">
              Sayfayı aç
            </Link>
          )}
          <button type="button" onClick={() => setEditing((v) => !v)} aria-expanded={editing} className="btn" title="Düzenle">
            <EditIcon />
            <span className="sr-only">Düzenle</span>
          </button>
          <form
            action={deleteStatusPageAction}
            onSubmit={(e) => {
              if (!confirm(`"${page.title}" durum sayfası silinsin mi? Monitor'lar silinmez.`)) e.preventDefault();
            }}
          >
            <input type="hidden" name="id" value={page.id} />
            <button type="submit" className="btn btn-danger" title="Sil">
              <TrashIcon />
              <span className="sr-only">Sil</span>
            </button>
          </form>
        </div>
      </div>
      {editing && (
        <div className="mt-3 flex flex-col gap-4 rounded-lg bg-sunken/60 p-4">
          <StatusPageForm page={page} monitors={monitors} onDone={() => setEditing(false)} />
          {page.published && page.monitors.length > 0 && (
            <details className="text-xs text-muted">
              <summary className="cursor-pointer font-medium">Rozet adresleri (README ya da wiki için)</summary>
              <ul className="mt-2 flex flex-col gap-1 font-mono">
                {page.monitors.map((m) => (
                  <li key={m.monitor_id}>
                    {m.display_name || labels.get(m.monitor_id)}: /api/badge/{m.monitor_id}/status ·
                    /api/badge/{m.monitor_id}/uptime
                  </li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}
    </li>
  );
}
