import Link from "next/link";
import { getT } from "@/lib/i18n/server";
import { CopyLine } from "@/app/ui/copy-line";
import { DiscoveryIcon, PlusIcon } from "@/app/ui/icons";

/**
 * What a page or a part of one says while it has nothing to show: an icon
 * in an accent tile, a title, a sentence or two, and the one thing to do
 * about it. No illustration. bare leaves out the panel, for a part that
 * sits in one already.
 */
export function EmptyState({
  icon,
  title,
  children,
  action,
  bare = false,
  headingLevel = 2,
}: {
  icon: React.ReactNode;
  title: string;
  children?: React.ReactNode;
  action?: React.ReactNode;
  bare?: boolean;
  headingLevel?: 1 | 2 | 3;
}) {
  const Heading = `h${headingLevel}` as const;
  return (
    <section className={`pg-empty ${bare ? "" : "panel"}`}>
      <span className="pg-empty-icon" aria-hidden>
        {icon}
      </span>
      <Heading className="pg-empty-title">{title}</Heading>
      {children && <div className="pg-empty-body">{children}</div>}
      {action && <div className="pg-empty-action">{action}</div>}
    </section>
  );
}

/** What an annotation that asks for monitoring reads in a manifest. */
export const MONITOR_ANNOTATION = 'kubeat.io/monitor: "true"';

/**
 * A page that needs monitors before it can do anything, while there are
 * none: the three ways in, cluster first. Discover the Services, Routes
 * and Ingresses of a namespace; annotate an object in its manifest; or add
 * an address by hand. Viewers, who can do none of these, learn why the page
 * is empty instead.
 */
export async function NoMonitors({ canEdit }: { canEdit: boolean }) {
  const t = await getT();
  if (!canEdit) {
    return (
      <EmptyState icon={<DiscoveryIcon width={20} height={20} />} title={t("noMonitors.viewerTitle")}>
        {t("noMonitors.viewerBody")}
      </EmptyState>
    );
  }
  return (
    <section className="pg-empty panel">
      <span className="pg-empty-icon" aria-hidden>
        <DiscoveryIcon width={20} height={20} />
      </span>
      <h2 className="pg-empty-title">{t("noMonitors.title")}</h2>
      <p className="pg-empty-body">{t("noMonitors.body")}</p>
      <ol className="pg-steps">
        <li>
          <h3 className="type-heading">{t("noMonitors.discoverTitle")}</h3>
          <p>{t("noMonitors.discoverBody")}</p>
          <Link href="/discovery" className="btn btn-primary self-start">
            <DiscoveryIcon />
            {t("noMonitors.discover")}
          </Link>
        </li>
        <li>
          <h3 className="type-heading">{t("noMonitors.annotateTitle")}</h3>
          <p>{t("noMonitors.annotateBody")}</p>
          <CopyLine text={MONITOR_ANNOTATION} />
        </li>
        <li>
          <h3 className="type-heading">{t("noMonitors.manualTitle")}</h3>
          <p>{t("noMonitors.manualBody")}</p>
          <Link href="/monitors/new" className="btn self-start">
            <PlusIcon />
            {t("noMonitors.add")}
          </Link>
        </li>
      </ol>
    </section>
  );
}
