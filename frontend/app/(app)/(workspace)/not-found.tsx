import { getT } from "@/lib/i18n/server";
import { NotFound } from "@/app/ui/empty-state";

/** An address in the workspace that leads nowhere, answered beside the monitor list. */
export default async function WorkspaceNotFound() {
  const t = await getT();
  return <NotFound title={t("notFound.title")} body={t("notFound.body")} back={{ href: "/", label: t("notFound.home") }} />;
}
