import Link from "next/link";
import { getT } from "@/lib/i18n/server";
import { CopyText } from "@/app/ui/copy-text";
import { DiscoveryIcon, PlusIcon } from "@/app/ui/icons";

/** What an annotation that asks for monitoring reads in a manifest. */
const ANNOTATION = 'kubeat.io/monitor: "true"';

/**
 * The overview before there is anything to watch. Editors get the three
 * ways in, cluster first: discover the Services, Routes and Ingresses;
 * annotate an object in its manifest; or add an address by hand. Viewers
 * learn why they see nothing.
 */
export async function EmptyFleet({ canEdit }: { canEdit: boolean }) {
  const t = await getT();
  return (
    <section className="panel fl-empty" aria-labelledby="fl-empty-title">
      <span className="fl-empty-icon" aria-hidden>
        <DiscoveryIcon width={20} height={20} />
      </span>
      <h1 id="fl-empty-title" className="type-title mt-4">
        {canEdit ? t("monitorsList.overview.empty.title") : t("monitorsList.overview.empty.viewerTitle")}
      </h1>
      <p className="mt-1.5 max-w-xl text-sm text-ink-2">
        {canEdit ? t("monitorsList.overview.empty.body") : t("monitorsList.overview.empty.viewerBody")}
      </p>
      {canEdit && (
        <ol className="fl-steps">
          <li>
            <h2 className="type-heading">{t("monitorsList.overview.empty.discoverTitle")}</h2>
            <p>{t("monitorsList.overview.empty.discoverBody")}</p>
            <Link href="/discovery" className="btn btn-primary mt-auto self-start">
              {t("monitorsList.overview.empty.discover")}
            </Link>
          </li>
          <li>
            <h2 className="type-heading">{t("monitorsList.overview.empty.annotateTitle")}</h2>
            <p>{t("monitorsList.overview.empty.annotateBody")}</p>
            <CopyText text={ANNOTATION} />
          </li>
          <li>
            <h2 className="type-heading">{t("monitorsList.overview.empty.manualTitle")}</h2>
            <p>{t("monitorsList.overview.empty.manualBody")}</p>
            <Link href="/monitors/new" className="btn mt-auto self-start">
              <PlusIcon />
              {t("monitorsList.overview.empty.addManually")}
            </Link>
          </li>
        </ol>
      )}
    </section>
  );
}
