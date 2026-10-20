"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";
import { deleteStatusPageAction, saveStatusPageAction } from "@/app/actions";
import type { StatusPage } from "@/lib/api";
import { useT } from "@/lib/i18n/client";
import { MANUAL_LABEL } from "@/lib/namespaces";
import { useMounted } from "@/lib/use-mounted";
import { ConfirmForm } from "@/app/ui/confirm";
import { EditIcon, TrashIcon } from "@/app/ui/icons";
import { AnnounceIcon, OpenIcon } from "@/app/ui/page-icons";

/** A monitor a status page can show. */
export type PageMonitorOption = { id: number; label: string; namespace: string };

/** What a slug may be: lowercase letters, digits and inner hyphens. */
const SLUG = "[a-z0-9]([a-z0-9\\-]*[a-z0-9])?";

/**
 * The form of a status page. With a page it edits that one; without, it
 * creates one. Its sections are the page itself (read back as its
 * address), an announcement, the services it shows, and whether it is
 * published; the save bar stays in reach while the long list scrolls by.
 * closeHref is where Cancel and a successful save lead.
 */
export function StatusPageForm({
  page,
  monitors,
  needsMonitor,
  closeHref,
}: {
  page?: StatusPage;
  monitors: PageMonitorOption[];
  /** Set for all but administrators: a page without monitors is theirs alone. */
  needsMonitor: boolean;
  closeHref: string;
}) {
  const t = useT();
  const router = useRouter();
  const mounted = useMounted();
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const [slug, setSlug] = useState(page?.slug ?? "");
  const [published, setPublished] = useState(page?.published ?? true);
  const [chosen, setChosen] = useState<Set<number>>(() => new Set(page?.monitors.map((m) => m.monitor_id)));
  const names = new Map(page?.monitors.map((m) => [m.monitor_id, m.display_name]));

  const groups = new Map<string, PageMonitorOption[]>();
  for (const m of monitors) groups.set(m.namespace, [...(groups.get(m.namespace) ?? []), m]);

  function toggle(id: number) {
    setChosen((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }

  // Submitted by hand: React resets a form after its action, which would
  // throw away what was typed into a rejected one.
  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    setError(null);
    startTransition(async () => {
      const result = await saveStatusPageAction(data);
      if (result?.error) {
        setError(result.error);
        return;
      }
      router.replace(closeHref, { scroll: false });
    });
  }

  const validSlug = new RegExp(`^${SLUG}$`).test(slug);
  const [addressBefore, addressAfter] = t("statusPageForm.echo.address").split("{url}");

  return (
    <form onSubmit={submit} className="pg-form">
      {page && <input type="hidden" name="id" value={page.id} />}

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.page")}</h2>
          <p>{t("statusPageForm.sec.pageBody")}</p>
        </div>
        <div className="pg-fields">
          <div className="pg-grid">
            <label className="pg-span-3">
              <span className="pg-label">{t("statusPageForm.title.label")}</span>
              <input
                name="title"
                required
                maxLength={100}
                defaultValue={page?.title}
                placeholder={t("statusPageForm.title.placeholder")}
                className="field"
              />
            </label>
            <label className="pg-span-3">
              <span className="pg-label">{t("statusPageForm.slug.label")}</span>
              <span className="pg-affix">
                <span>/status/</span>
                <input
                  name="slug"
                  required
                  pattern={SLUG}
                  title={t("statusPageForm.slug.title")}
                  value={slug}
                  onChange={(e) => setSlug(e.target.value)}
                  placeholder={t("statusPageForm.slug.placeholder")}
                  className="field font-mono"
                />
              </span>
            </label>
          </div>
          <label>
            <span className="pg-label">
              {t("statusPageForm.description.label")}
              <small>{t("form.optional")}</small>
            </span>
            <input
              name="description"
              defaultValue={page?.description}
              placeholder={t("statusPageForm.description.placeholder")}
              className="field"
            />
          </label>
          {mounted && validSlug && (
            <p className="pg-echo" aria-live="polite">
              {addressBefore}
              <code className="break-all">{`${window.location.origin}/status/${slug}`}</code>
              {addressAfter}
            </p>
          )}
        </div>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.announcement")}</h2>
          <p>{t("statusPageForm.sec.announcementBody")}</p>
        </div>
        <div className="pg-fields">
          <label>
            <span className="pg-label">
              {t("statusPageForm.announcement.label")}
              <small>{t("form.optional")}</small>
            </span>
            <textarea
              name="announcement"
              rows={3}
              defaultValue={page?.announcement}
              placeholder={t("statusPageForm.announcement.placeholder")}
              className="field"
            />
            <span className="pg-help">{t("statusPageForm.announcement.hint")}</span>
          </label>
        </div>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.services")}</h2>
          <p>{t("statusPageForm.sec.servicesBody")}</p>
        </div>
        <fieldset className="pg-fields">
          <legend className="sr-only">{t("statusPageForm.monitors.legend")}</legend>
          {monitors.length === 0 ? (
            <p className="pg-help mt-0">{t("statusPageForm.monitors.empty")}</p>
          ) : (
            <div className="pg-picker">
              {[...groups.entries()].map(([namespace, group]) => (
                <div key={namespace} role="group" aria-label={namespace || t(MANUAL_LABEL)}>
                  <div className={`pg-picker-head ${namespace ? "pg-mono" : ""}`}>{namespace || t(MANUAL_LABEL)}</div>
                  {group.map((m) => (
                    <div key={m.id} className="pg-picker-row">
                      <label className="pg-picker-pick">
                        <input type="checkbox" name="monitor" value={m.id} checked={chosen.has(m.id)} onChange={() => toggle(m.id)} />
                        <span className="pg-trunc" title={m.label}>
                          {m.label}
                        </span>
                      </label>
                      {chosen.has(m.id) && (
                        <input
                          name={`name_${m.id}`}
                          defaultValue={names.get(m.id)}
                          maxLength={100}
                          aria-label={t("statusPageForm.monitors.displayName.aria", { label: m.label })}
                          placeholder={t("statusPageForm.monitors.displayName.placeholder")}
                          className="field pg-picker-name"
                        />
                      )}
                    </div>
                  ))}
                </div>
              ))}
            </div>
          )}
          <p className="pg-echo" aria-live="polite">
            {chosen.size === 0 ? t("statusPageForm.echo.services.none") : t.n("statusPageForm.echo.services", chosen.size)}{" "}
            {t("statusPageForm.monitors.hint")}
            {needsMonitor && chosen.size === 0 && <span className="mt-1 block">{t("statusPageForm.monitors.needed")}</span>}
          </p>
        </fieldset>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.publish")}</h2>
          <p>{t("statusPageForm.sec.publishBody")}</p>
        </div>
        <div className="pg-fields">
          <label className="pg-check">
            <input type="checkbox" name="published" checked={published} onChange={(e) => setPublished(e.target.checked)} />
            <span>
              <b>{t("statusPageForm.published.label")}</b>
              <small>{published ? t("statusPageForm.published.on") : t("statusPageForm.published.off")}</small>
            </span>
          </label>
        </div>
      </div>

      <div className="pg-savebar pg-savebar-sticky">
        {error && <p role="alert">{error}</p>}
        <div className="pg-savebar-end">
          <Link href={closeHref} scroll={false} className="btn btn-ghost">
            {t("common.cancel")}
          </Link>
          <button type="submit" disabled={pending} className="btn btn-primary">
            {pending ? t("common.saving") : page ? t("common.save") : t("statusPageForm.create")}
          </button>
        </div>
      </div>
    </form>
  );
}

/**
 * One status page as a row of the table: its title, its address, how many
 * services it shows, whether it is published, and what can be done with
 * it. While it is edited its form opens in a row of its own under it, with
 * the badge addresses of its services.
 */
export function StatusPageRow({
  page,
  monitors,
  needsMonitor,
  canEdit,
  editing,
}: {
  page: StatusPage;
  monitors: PageMonitorOption[];
  needsMonitor: boolean;
  canEdit: boolean;
  editing: boolean;
}) {
  const t = useT();
  const labels = new Map(monitors.map((m) => [m.id, m.label]));
  const address = `/status/${page.slug}`;

  return (
    <>
      <tr className={editing ? "pg-row-open" : undefined}>
        <td className="pg-grow">
          <span className="flex min-w-0 items-center gap-2">
            <span className="pg-trunc pg-name" title={page.title}>
              {page.title}
            </span>
            {page.announcement && (
              <span className="flex-none text-ink-3" title={t("statusPageRow.hasAnnouncement")}>
                <AnnounceIcon width={14} height={14} />
                <span className="sr-only">{t("statusPageRow.hasAnnouncement")}</span>
              </span>
            )}
          </span>
        </td>
        <td className="pg-shrink">
          {page.published ? (
            <a href={address} target="_blank" rel="noopener" className="pg-trunc pg-mono max-w-72 text-ink-2 hover:text-ink hover:underline" title={address}>
              {address}
            </a>
          ) : (
            <span className="pg-trunc pg-mono max-w-72 text-ink-3" title={address}>
              {address}
            </span>
          )}
        </td>
        <td className="pg-num pg-shrink" data-label={`${t("statusPages.col.services")}:`}>
          {page.monitors.length}
        </td>
        <td className="pg-shrink">
          {page.published ? (
            <span className="text-ink">{t("statusPageRow.published")}</span>
          ) : (
            <span className="pg-tag">{t("statusPageRow.draft")}</span>
          )}
        </td>
        <td className="pg-actions">
          <span className="pg-row-actions">
            {page.published && (
              <a
                href={address}
                target="_blank"
                rel="noopener"
                className="pg-icon-btn"
                aria-label={`${t("statusPageRow.open")}: ${page.title}`}
                title={t("statusPageRow.openTitle")}
              >
                <OpenIcon />
              </a>
            )}
            {canEdit && (
              <>
                <Link
                  href={editing ? "/status-pages" : `/status-pages?edit=${page.id}`}
                  scroll={false}
                  className="pg-icon-btn"
                  aria-label={`${t("common.edit")}: ${page.title}`}
                  aria-expanded={editing}
                  title={t("common.edit")}
                >
                  <EditIcon />
                </Link>
                <ConfirmForm
                  action={deleteStatusPageAction}
                  className="contents"
                  title={t("statusPageRow.delete.title", { title: page.title })}
                  question={t("statusPageRow.delete.body")}
                  items={[`${address} · ${t.n("statusPageRow.monitors", page.monitors.length)}`]}
                  icon={<TrashIcon />}
                >
                  <input type="hidden" name="id" value={page.id} />
                  <button
                    type="submit"
                    className="pg-icon-btn pg-icon-btn-danger"
                    aria-label={`${t("common.delete")}: ${page.title}`}
                    title={t("common.delete")}
                  >
                    <TrashIcon />
                  </button>
                </ConfirmForm>
              </>
            )}
          </span>
        </td>
      </tr>
      {editing && (
        <tr className="pg-row-form">
          <td colSpan={5}>
            <div className="pg-inline-form">
              <StatusPageForm page={page} monitors={monitors} needsMonitor={needsMonitor} closeHref="/status-pages" />
              {page.published && page.monitors.length > 0 && (
                <details className="pg-badges">
                  <summary>{t("statusPageRow.badges.summary")}</summary>
                  <ul>
                    {page.monitors.map((m) => (
                      <li key={m.monitor_id}>
                        <span className="pg-trunc">{m.display_name || labels.get(m.monitor_id)}</span>
                        <code>/api/badge/{m.monitor_id}/status</code>
                        <code>/api/badge/{m.monitor_id}/uptime</code>
                      </li>
                    ))}
                  </ul>
                </details>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
