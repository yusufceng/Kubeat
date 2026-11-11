import Link from "next/link";
import { getT } from "@/lib/i18n/server";
import { CopyText } from "@/app/ui/copy-text";
import { DiscoveryIcon, PlusIcon } from "@/app/ui/icons";

/** What an annotation that asks for monitoring reads in a manifest. */
const ANNOTATION = 'kubeat.io/monitor: "true"';

/**
 * The overview before there is anything to watch: one sentence and the way
 * in, cluster first (discover the Services, Routes and Ingresses), with
 * the other two ways on a line below: add an address by hand, or annotate
 * an object in its manifest. Viewers learn why they see nothing.
 */
export async function EmptyFleet({ canEdit }: { canEdit: boolean }) {
  const t = await getT();
  return (
    <section className="panel fl-empty" aria-labelledby="fl-empty-title">
      <h1 id="fl-empty-title" className="type-title">
        {canEdit ? t("monitorsList.overview.empty.title") : t("monitorsList.overview.empty.viewerTitle")}
      </h1>
      <p className="max-w-2xl text-sm text-ink-2">
        {canEdit ? t("monitorsList.overview.empty.lead") : t("monitorsList.overview.empty.viewerBody")}
      </p>
      {canEdit && (
        <div className="fl-empty-actions">
          <Link href="/discovery" className="btn btn-primary">
            <DiscoveryIcon />
            {t("monitorsList.overview.empty.discover")}
          </Link>
          <Link href="/monitors/new" className="btn">
            <PlusIcon />
            {t("monitorsList.overview.empty.addManually")}
          </Link>
          <span className="fl-empty-or">
            {t("monitorsList.overview.empty.annotateInline")}
            <CopyText text={ANNOTATION} />
          </span>
        </div>
      )}
    </section>
  );
}
