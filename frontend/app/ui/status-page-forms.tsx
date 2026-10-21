"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useId, useRef, useState, useTransition } from "react";
import { deleteStatusPageAction } from "@/app/actions";
import { savePageAction } from "@/app/(app)/(pages)/status-pages/actions";
import type { StatusPage, StatusPageLanguage, StatusPagePreview, StatusPageSettings, StatusPageUpdate } from "@/lib/api";
import { useT } from "@/lib/i18n/client";
import type { T } from "@/lib/i18n/translate";
import { MANUAL_LABEL } from "@/lib/namespaces";
import { useMounted } from "@/lib/use-mounted";
import { ConfirmForm } from "@/app/ui/confirm";
import { ChevronIcon, EditIcon, PlusIcon, RenewIcon, TrashIcon } from "@/app/ui/icons";
import { AnnounceIcon, CloseIcon, LockIcon, OpenIcon } from "@/app/ui/page-icons";
import { StatusGlyph } from "@/app/ui/status-glyph";
import { type IncidentOption, StatusPageUpdates } from "@/app/ui/status-page-updates";

/** A monitor a status page can show. */
export type PageMonitorOption = { id: number; label: string; namespace: string; tags: string[] };

/** What the editor of an existing page is handed besides the page: what it shows now, its updates, and the incidents an update can point at. */
export type PageExtras = {
  preview: StatusPagePreview | null;
  updates: StatusPageUpdate[] | null;
  incidents: IncidentOption[];
};

/** What a slug may be: lowercase letters, digits and inner hyphens. */
const SLUG = "[a-z0-9]([a-z0-9\\-]*[a-z0-9])?";

/** A section while it is edited: a key that survives renaming, so that monitors and rules can point at it. */
type Section = { key: number; name: string };
/** A chosen monitor: the name the page shows (empty for its own) and the section it is in (null for the top group). */
type Pick = { name: string; section: number | null };
type Rule = { key: number; namespace: string; tag: string; section: number | null };

const languages: { value: StatusPageLanguage; label: string }[] = [
  { value: "", label: "statusPageForm.language.default" },
  { value: "tr", label: "statusPageForm.language.tr" },
  { value: "en", label: "statusPageForm.language.en" },
];

/** The sections of a page as the form holds them, and its monitors and rules pointing at them by key. */
function fromPage(page?: StatusPage): { sections: Section[]; chosen: Map<number, Pick>; rules: Rule[] } {
  const sections = (page?.sections ?? []).map((s, i) => ({ key: i + 1, name: s.name }));
  const keyOf = (name: string) => sections.find((s) => s.name === name)?.key ?? null;
  return {
    sections,
    chosen: new Map((page?.monitors ?? []).map((m) => [m.monitor_id, { name: m.display_name, section: keyOf(m.section) }])),
    rules: (page?.rules ?? []).map((r, i) => ({ key: 1000 + i, namespace: r.namespace, tag: r.tag, section: keyOf(r.section) })),
  };
}

/** A rule read back as a sentence: "every monitor in checkout tagged tier-1, in the Payments section". */
function ruleWords(rule: Rule, sections: Section[], t: T): React.ReactNode {
  const section = rule.section === null ? null : sections.find((s) => s.key === rule.section);
  const who = rule.namespace
    ? rule.tag
      ? t("statusPageForm.rule.who.nsTag", { namespace: rule.namespace, tag: rule.tag })
      : t("statusPageForm.rule.who.ns", { namespace: rule.namespace })
    : rule.tag
      ? t("statusPageForm.rule.who.tag", { tag: rule.tag })
      : t("statusPageForm.rule.who.all");
  const where = section ? t("statusPageForm.rule.where.section", { section: section.name || t("statusPageForm.sections.unnamed") }) : t("statusPageForm.rule.where.top");
  return t("statusPageForm.rule.echo", { who, where });
}

/** The select a monitor or a rule picks its section with. */
function SectionSelect({
  sections,
  value,
  onChange,
  label,
}: {
  sections: Section[];
  value: number | null;
  onChange: (key: number | null) => void;
  label: string;
}) {
  const t = useT();
  return (
    <select
      className="field pg-section-select"
      value={value ?? ""}
      onChange={(e) => onChange(e.target.value === "" ? null : Number(e.target.value))}
      aria-label={label}
    >
      <option value="">{t("statusPageForm.sections.top")}</option>
      {sections.map((s) => (
        <option key={s.key} value={s.key}>
          {s.name || t("statusPageForm.sections.unnamed")}
        </option>
      ))}
    </select>
  );
}

/** What the page shows at this moment, as saved: each section with its monitors, those a rule matched marked so. */
function Preview({ preview }: { preview: StatusPagePreview | null }) {
  const t = useT();
  const router = useRouter();
  const [refreshing, startRefresh] = useTransition();
  const total = preview?.sections.reduce((n, s) => n + s.monitors.length, 0) ?? 0;
  return (
    <div className="pg-sec">
      <div className="pg-sec-intro">
        <h2>{t("statusPageForm.sec.preview")}</h2>
        <p>{t("statusPageForm.sec.previewBody")}</p>
      </div>
      <div className="pg-fields">
        <div className="pg-preview-bar">
          <span className="text-ink-2">{preview ? t.n("statusPageForm.preview.count", total) : t("statusPageForm.preview.error")}</span>
          <button type="button" className="btn btn-ghost btn-sm" disabled={refreshing} onClick={() => startRefresh(() => router.refresh())}>
            <RenewIcon width={14} height={14} />
            {t("statusPageForm.preview.refresh")}
          </button>
        </div>
        {preview && (
          <div className="pg-preview" aria-label={t("statusPageForm.sec.preview")}>
            {preview.sections.map((sec) => (
              <div key={sec.name} className="pg-preview-sec">
                <div className="pg-preview-head">
                  <span className={sec.name ? "" : "text-ink-3"}>{sec.name || t("statusPageForm.sections.top")}</span>
                  <span className="pg-count">{sec.monitors.length}</span>
                </div>
                {sec.monitors.length === 0 ? (
                  <p className="pg-preview-empty">{t("statusPageForm.preview.emptySection")}</p>
                ) : (
                  sec.monitors.map((m) => (
                    <div key={m.monitor_id} className={`pg-preview-row ${m.enabled ? "" : "pg-preview-paused"}`}>
                      {m.enabled ? <span className="pg-preview-dot" aria-hidden /> : <StatusGlyph kind="paused" label={t("statusPageForm.preview.paused")} />}
                      <span className="pg-preview-name" title={m.name}>
                        {m.name}
                      </span>
                      <span className="pg-preview-source" title={`${m.monitor_name}${m.namespace ? ` · ${m.namespace}` : ""}`}>
                        {m.monitor_name}
                        {m.namespace && <span className="text-ink-3"> · {m.namespace}</span>}
                      </span>
                      <span className={`pg-tag ${m.explicit ? "" : "pg-tag-rule"}`}>
                        {m.explicit ? t("statusPageForm.preview.explicit") : t("statusPageForm.preview.matched")}
                      </span>
                    </div>
                  ))
                )}
              </div>
            ))}
          </div>
        )}
        <span className="pg-help">{t("statusPageForm.preview.hint")}</span>
      </div>
    </div>
  );
}

/**
 * The form of a status page. With a page it edits that one; without, it
 * creates one. Its sections: the page itself (read back as its address),
 * an announcement, the page's sections, the services with the section and
 * name of each, the rules that add services by namespace and tag, what the
 * saved page shows now, its settings (indexing, paused services, language,
 * detail pages, password) and whether it is published. Nothing is flattened
 * on save: every monitor goes with its section. The save bar stays in
 * reach while the long form scrolls by. closeHref is where Cancel and a
 * successful save lead.
 */
export function StatusPageForm({
  page,
  monitors,
  needsMonitor,
  anyNamespace,
  preview,
  closeHref,
}: {
  page?: StatusPage;
  monitors: PageMonitorOption[];
  /** Set for all but administrators: a page without monitors or rules is theirs alone. */
  needsMonitor: boolean;
  /** Whether a rule may say "any namespace": for those who edit every namespace. */
  anyNamespace: boolean;
  /** What the saved page shows now; null when it could not be read; absent for a new page. */
  preview?: StatusPagePreview | null;
  closeHref: string;
}) {
  const t = useT();
  const router = useRouter();
  const mounted = useMounted();
  const ids = useId();
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const [slug, setSlug] = useState(page?.slug ?? "");
  const [published, setPublished] = useState(page?.published ?? true);
  const [initial] = useState(() => fromPage(page));
  const [sections, setSections] = useState<Section[]>(initial.sections);
  const [chosen, setChosen] = useState<Map<number, Pick>>(initial.chosen);
  const [rules, setRules] = useState<Rule[]>(initial.rules);
  const [noindex, setNoindex] = useState(page?.noindex ?? false);
  const [hidePaused, setHidePaused] = useState(page?.hide_paused ?? false);
  const [language, setLanguage] = useState<StatusPageLanguage>(page?.language ?? "");
  const [detailPages, setDetailPages] = useState(page?.detail_pages ?? true);
  const [password, setPassword] = useState("");
  const [clearPassword, setClearPassword] = useState(false);
  // Keys for sections and rules added while editing; never reused.
  const nextKey = useRef(2000);
  const newKey = () => nextKey.current++;

  const groups = new Map<string, PageMonitorOption[]>();
  for (const m of monitors) groups.set(m.namespace, [...(groups.get(m.namespace) ?? []), m]);
  const namespaces = [...groups.keys()].filter(Boolean);
  const tags = [...new Set(monitors.flatMap((m) => m.tags))].sort((a, b) => a.localeCompare(b, t.intl));

  function toggle(id: number) {
    setChosen((prev) => {
      const next = new Map(prev);
      if (!next.delete(id)) next.set(id, { name: "", section: null });
      return next;
    });
  }
  function setPick(id: number, change: Partial<Pick>) {
    setChosen((prev) => {
      const next = new Map(prev);
      const current = next.get(id);
      if (current) next.set(id, { ...current, ...change });
      return next;
    });
  }

  function addSection() {
    setSections((prev) => [...prev, { key: newKey(), name: "" }]);
  }
  function renameSection(key: number, name: string) {
    setSections((prev) => prev.map((s) => (s.key === key ? { ...s, name } : s)));
  }
  function moveSection(key: number, by: -1 | 1) {
    setSections((prev) => {
      const i = prev.findIndex((s) => s.key === key);
      const j = i + by;
      if (i < 0 || j < 0 || j >= prev.length) return prev;
      const next = [...prev];
      [next[i], next[j]] = [next[j], next[i]];
      return next;
    });
  }
  // What was in a removed section moves to the top group: the backend
  // refuses a monitor or rule that names a section the page lacks.
  function removeSection(key: number) {
    setSections((prev) => prev.filter((s) => s.key !== key));
    setChosen((prev) => new Map([...prev].map(([id, p]) => [id, p.section === key ? { ...p, section: null } : p])));
    setRules((prev) => prev.map((r) => (r.section === key ? { ...r, section: null } : r)));
  }

  function addRule() {
    setRules((prev) => [...prev, { key: newKey(), namespace: anyNamespace ? "" : (namespaces[0] ?? ""), tag: "", section: null }]);
  }
  function setRule(key: number, change: Partial<Rule>) {
    setRules((prev) => prev.map((r) => (r.key === key ? { ...r, ...change } : r)));
  }

  const sectionNames = sections.map((s) => s.name.trim());
  const sectionsValid = sectionNames.every(Boolean) && new Set(sectionNames).size === sectionNames.length;
  const empty = chosen.size === 0 && rules.length === 0;

  // Submitted by hand: React resets a form after its action, which would
  // throw away what was typed into a rejected one.
  function submit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!sectionsValid) {
      setError(t("statusPageForm.sections.invalid"));
      return;
    }
    const data = new FormData(e.currentTarget);
    const text = (name: string) => String(data.get(name) ?? "").trim();
    const nameOf = (key: number | null) => (key === null ? "" : (sections.find((s) => s.key === key)?.name.trim() ?? ""));
    const settings: Partial<StatusPageSettings> = {
      slug: text("slug"),
      title: text("title"),
      description: text("description"),
      announcement: text("announcement"),
      published,
      sections: sectionNames.map((name) => ({ name })),
      // The chosen monitors in the order the form lists them, each with its section.
      monitors: monitors
        .filter((m) => chosen.has(m.id))
        .map((m) => ({ monitor_id: m.id, display_name: chosen.get(m.id)!.name.trim(), section: nameOf(chosen.get(m.id)!.section) })),
      rules: rules.map((r) => ({ namespace: r.namespace, tag: r.tag.trim(), section: nameOf(r.section) })),
      noindex,
      hide_paused: hidePaused,
      language,
      detail_pages: detailPages,
      // The password goes only when it changes: "" clears it, a word sets it.
      ...(clearPassword ? { password: "" } : password ? { password } : {}),
    };
    setError(null);
    startTransition(async () => {
      const result = await savePageAction(page?.id ?? null, settings);
      if (result?.error) {
        setError(result.error);
        return;
      }
      router.replace(closeHref, { scroll: false });
    });
  }

  const validSlug = new RegExp(`^${SLUG}$`).test(slug);
  const [addressBefore, addressAfter] = t("statusPageForm.echo.address").split("{url}");
  const hasPassword = clearPassword ? false : Boolean(password) || Boolean(page?.has_password);
  const settingsEcho = [
    language === "" ? t("statusPageForm.echo.language.default") : t("statusPageForm.echo.language", { language: t(languages.find((l) => l.value === language)!.label) }),
    noindex ? t("statusPageForm.echo.noindex.on") : t("statusPageForm.echo.noindex.off"),
    hidePaused ? t("statusPageForm.echo.hidePaused.on") : t("statusPageForm.echo.hidePaused.off"),
    detailPages ? t("statusPageForm.echo.detail.on") : t("statusPageForm.echo.detail.off"),
    hasPassword ? t("statusPageForm.echo.password.on") : t("statusPageForm.echo.password.off"),
  ].join(" · ");

  return (
    <form onSubmit={submit} className="pg-form">
      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.page")}</h2>
          <p>{t("statusPageForm.sec.pageBody")}</p>
        </div>
        <div className="pg-fields">
          <div className="pg-grid">
            <label className="pg-span-3">
              <span className="pg-label">{t("statusPageForm.title.label")}</span>
              <input name="title" required maxLength={100} defaultValue={page?.title} placeholder={t("statusPageForm.title.placeholder")} className="field" />
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
            <input name="description" defaultValue={page?.description} placeholder={t("statusPageForm.description.placeholder")} className="field" />
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
            <textarea name="announcement" rows={3} defaultValue={page?.announcement} placeholder={t("statusPageForm.announcement.placeholder")} className="field" />
            <span className="pg-help">{t("statusPageForm.announcement.hint")}</span>
          </label>
        </div>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.sections")}</h2>
          <p>{t("statusPageForm.sec.sectionsBody")}</p>
        </div>
        <fieldset className="pg-fields">
          <legend className="sr-only">{t("statusPageForm.sec.sections")}</legend>
          {sections.length > 0 && (
            <ol className="pg-sections" aria-label={t("statusPageForm.sections.list")}>
              {sections.map((s, i) => (
                <li key={s.key} className="pg-section-row">
                  <span className="pg-section-no" aria-hidden>
                    {i + 1}
                  </span>
                  <input
                    value={s.name}
                    onChange={(e) => renameSection(s.key, e.target.value)}
                    maxLength={100}
                    required
                    placeholder={t("statusPageForm.sections.placeholder")}
                    aria-label={t("statusPageForm.sections.name", { n: i + 1 })}
                    className="field"
                  />
                  <span className="pg-row-actions">
                    <button type="button" className="pg-icon-btn" disabled={i === 0} onClick={() => moveSection(s.key, -1)} aria-label={t("statusPageForm.sections.up", { name: s.name })} title={t("statusPageForm.sections.upTitle")}>
                      <ChevronIcon className="-rotate-90" />
                    </button>
                    <button type="button" className="pg-icon-btn" disabled={i === sections.length - 1} onClick={() => moveSection(s.key, 1)} aria-label={t("statusPageForm.sections.down", { name: s.name })} title={t("statusPageForm.sections.downTitle")}>
                      <ChevronIcon className="rotate-90" />
                    </button>
                    <button type="button" className="pg-icon-btn pg-icon-btn-danger" onClick={() => removeSection(s.key)} aria-label={t("statusPageForm.sections.remove", { name: s.name })} title={t("statusPageForm.sections.removeTitle")}>
                      <CloseIcon />
                    </button>
                  </span>
                </li>
              ))}
            </ol>
          )}
          <div>
            <button type="button" className="btn btn-sm" onClick={addSection}>
              <PlusIcon width={14} height={14} />
              {t("statusPageForm.sections.add")}
            </button>
          </div>
          <p className="pg-help mt-0">
            {sections.length === 0 ? t("statusPageForm.sections.none") : t("statusPageForm.sections.hint")}
            {!sectionsValid && <span className="mt-1 block text-down-ink">{t("statusPageForm.sections.invalid")}</span>}
          </p>
        </fieldset>
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
                  {group.map((m) => {
                    const pick = chosen.get(m.id);
                    return (
                      <div key={m.id} className="pg-picker-row">
                        <label className="pg-picker-pick">
                          <input type="checkbox" checked={Boolean(pick)} onChange={() => toggle(m.id)} />
                          <span className="pg-trunc" title={m.label}>
                            {m.label}
                          </span>
                        </label>
                        {pick && (
                          <span className="pg-picker-fields">
                            <input
                              value={pick.name}
                              onChange={(e) => setPick(m.id, { name: e.target.value })}
                              maxLength={100}
                              aria-label={t("statusPageForm.monitors.displayName.aria", { label: m.label })}
                              placeholder={t("statusPageForm.monitors.displayName.placeholder")}
                              className="field pg-picker-name"
                            />
                            {sections.length > 0 && (
                              <SectionSelect
                                sections={sections}
                                value={pick.section}
                                onChange={(section) => setPick(m.id, { section })}
                                label={t("statusPageForm.monitors.section.aria", { label: m.label })}
                              />
                            )}
                          </span>
                        )}
                      </div>
                    );
                  })}
                </div>
              ))}
            </div>
          )}
          <p className="pg-echo" aria-live="polite">
            {chosen.size === 0 ? t("statusPageForm.echo.services.none") : t.n("statusPageForm.echo.services", chosen.size)} {t("statusPageForm.monitors.hint")}
            {needsMonitor && empty && <span className="mt-1 block">{t("statusPageForm.monitors.needed")}</span>}
          </p>
        </fieldset>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.rules")}</h2>
          <p>{t("statusPageForm.sec.rulesBody")}</p>
        </div>
        <fieldset className="pg-fields">
          <legend className="sr-only">{t("statusPageForm.sec.rules")}</legend>
          {rules.length > 0 && (
            <ol className="pg-rules" aria-label={t("statusPageForm.rules.list")}>
              {rules.map((r, i) => (
                <li key={r.key} className="pg-rule">
                  <div className="pg-rule-fields">
                    <select
                      className="field pg-mono"
                      value={r.namespace}
                      onChange={(e) => setRule(r.key, { namespace: e.target.value })}
                      aria-label={t("statusPageForm.rules.namespace", { n: i + 1 })}
                    >
                      {anyNamespace && <option value="">{t("statusPageForm.rules.anyNamespace")}</option>}
                      {namespaces.map((ns) => (
                        <option key={ns} value={ns}>
                          {ns}
                        </option>
                      ))}
                    </select>
                    <input
                      className="field pg-mono"
                      value={r.tag}
                      onChange={(e) => setRule(r.key, { tag: e.target.value })}
                      list={`${ids}-tags`}
                      maxLength={63}
                      placeholder={t("statusPageForm.rules.anyTag")}
                      aria-label={t("statusPageForm.rules.tag", { n: i + 1 })}
                    />
                    <SectionSelect sections={sections} value={r.section} onChange={(section) => setRule(r.key, { section })} label={t("statusPageForm.rules.section", { n: i + 1 })} />
                    <button
                      type="button"
                      className="pg-icon-btn pg-icon-btn-danger"
                      onClick={() => setRules((prev) => prev.filter((x) => x.key !== r.key))}
                      aria-label={t("statusPageForm.rules.remove", { n: i + 1 })}
                      title={t("statusPageForm.rules.removeTitle")}
                    >
                      <CloseIcon />
                    </button>
                  </div>
                  <p className="pg-rule-echo">{ruleWords(r, sections, t)}</p>
                </li>
              ))}
            </ol>
          )}
          <datalist id={`${ids}-tags`}>
            {tags.map((tag) => (
              <option key={tag} value={tag} />
            ))}
          </datalist>
          <div>
            <button type="button" className="btn btn-sm" onClick={addRule} disabled={!anyNamespace && namespaces.length === 0}>
              <PlusIcon width={14} height={14} />
              {t("statusPageForm.rules.add")}
            </button>
          </div>
          <p className="pg-help mt-0">{rules.length === 0 ? t("statusPageForm.rules.none") : t("statusPageForm.rules.hint")}</p>
        </fieldset>
      </div>

      {page && <Preview preview={preview ?? null} />}

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.settings")}</h2>
          <p>{t("statusPageForm.sec.settingsBody")}</p>
        </div>
        <div className="pg-fields">
          <fieldset>
            <legend className="pg-label">{t("statusPageForm.language.label")}</legend>
            <div className="pg-seg">
              {languages.map((l) => (
                <label key={l.value}>
                  <input type="radio" name="language" value={l.value} checked={language === l.value} onChange={() => setLanguage(l.value)} />
                  {t(l.label)}
                </label>
              ))}
            </div>
            <span className="pg-help">{t("statusPageForm.language.hint")}</span>
          </fieldset>
          <label className="pg-check">
            <input type="checkbox" checked={noindex} onChange={(e) => setNoindex(e.target.checked)} />
            <span>
              <b>{t("statusPageForm.noindex.label")}</b>
              <small>{t("statusPageForm.noindex.hint")}</small>
            </span>
          </label>
          <label className="pg-check">
            <input type="checkbox" checked={hidePaused} onChange={(e) => setHidePaused(e.target.checked)} />
            <span>
              <b>{t("statusPageForm.hidePaused.label")}</b>
              <small>{t("statusPageForm.hidePaused.hint")}</small>
            </span>
          </label>
          <label className="pg-check">
            <input type="checkbox" checked={detailPages} onChange={(e) => setDetailPages(e.target.checked)} />
            <span>
              <b>{t("statusPageForm.detailPages.label")}</b>
              <small>{t("statusPageForm.detailPages.hint")}</small>
            </span>
          </label>
          <div className="pg-password">
            {page?.has_password && (
              <p className="pg-password-state">
                <LockIcon width={14} height={14} />
                {t("statusPageForm.password.set")}
              </p>
            )}
            <label>
              <span className="pg-label">
                {page?.has_password ? t("statusPageForm.password.change") : t("statusPageForm.password.label")}
                <small>{t("form.optional")}</small>
              </span>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={clearPassword}
                autoComplete="new-password"
                maxLength={72}
                placeholder={page?.has_password ? t("statusPageForm.password.changePlaceholder") : t("statusPageForm.password.placeholder")}
                className="field"
              />
              <span className="pg-help">{t("statusPageForm.password.hint")}</span>
            </label>
            {page?.has_password && (
              <label className="pg-check">
                <input type="checkbox" checked={clearPassword} onChange={(e) => setClearPassword(e.target.checked)} />
                <span>
                  <b>{t("statusPageForm.password.clear")}</b>
                  <small>{t("statusPageForm.password.clearHint")}</small>
                </span>
              </label>
            )}
          </div>
          <p className="pg-echo" aria-live="polite">
            {settingsEcho}
          </p>
        </div>
      </div>

      <div className="pg-sec">
        <div className="pg-sec-intro">
          <h2>{t("statusPageForm.sec.publish")}</h2>
          <p>{t("statusPageForm.sec.publishBody")}</p>
        </div>
        <div className="pg-fields">
          <label className="pg-check">
            <input type="checkbox" checked={published} onChange={(e) => setPublished(e.target.checked)} />
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
 * services it lists by name and by rule, whether it is published or
 * protected, and what can be done with it. While it is edited its form
 * opens in a row of its own under it, with the updates it has told its
 * visitors and the badge addresses of its services.
 */
export function StatusPageRow({
  page,
  monitors,
  needsMonitor,
  anyNamespace,
  canEdit,
  editing,
  extras,
}: {
  page: StatusPage;
  monitors: PageMonitorOption[];
  needsMonitor: boolean;
  anyNamespace: boolean;
  canEdit: boolean;
  editing: boolean;
  /** Read only for the page being edited. */
  extras?: PageExtras;
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
            {page.has_password && (
              <span className="flex-none text-ink-3" title={t("statusPageRow.hasPassword")}>
                <LockIcon width={14} height={14} />
                <span className="sr-only">{t("statusPageRow.hasPassword")}</span>
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
          {page.rules.length > 0 && (
            <span className="text-ink-3" title={t.n("statusPageRow.rules", page.rules.length)}>
              {" "}
              + {t.n("statusPageRow.rulesShort", page.rules.length)}
            </span>
          )}
        </td>
        <td className="pg-shrink">
          {page.published ? <span className="text-ink">{t("statusPageRow.published")}</span> : <span className="pg-tag">{t("statusPageRow.draft")}</span>}
        </td>
        <td className="pg-actions">
          <span className="pg-row-actions">
            {page.published && (
              <a href={address} target="_blank" rel="noopener" className="pg-icon-btn" aria-label={`${t("statusPageRow.open")}: ${page.title}`} title={t("statusPageRow.openTitle")}>
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
                  <button type="submit" className="pg-icon-btn pg-icon-btn-danger" aria-label={`${t("common.delete")}: ${page.title}`} title={t("common.delete")}>
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
              <StatusPageForm page={page} monitors={monitors} needsMonitor={needsMonitor} anyNamespace={anyNamespace} preview={extras?.preview} closeHref="/status-pages" />
            </div>
            <div className="pg-inline-form mt-4">
              <StatusPageUpdates pageId={page.id} updates={extras?.updates ?? null} incidents={extras?.incidents ?? []} />
            </div>
            {page.published && page.monitors.length > 0 && (
              <div className="pg-inline-form mt-4">
                <details className="pg-badges pg-badges-own">
                  <summary>{t("statusPageRow.badges.summary")}</summary>
                  {page.has_password && <p className="pg-help">{t("statusPageRow.badges.locked")}</p>}
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
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}
