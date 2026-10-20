import { getT } from "@/lib/i18n/server";
import { NotFound } from "@/app/ui/empty-state";

/** A namespace without a monitor this viewer can see. */
export default async function NamespaceNotFound() {
  const t = await getT();
  return (
    <NotFound
      title={t("notFound.namespace.title")}
      body={t("notFound.namespace.body")}
      back={{ href: "/", label: t("notFound.home") }}
    />
  );
}
