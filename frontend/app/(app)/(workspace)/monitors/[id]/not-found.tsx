import { getT } from "@/lib/i18n/server";
import { NotFound } from "@/app/ui/empty-state";

/** A monitor that is not there (any more), or not for this viewer: its page and its edit form. */
export default async function MonitorNotFound() {
  const t = await getT();
  return (
    <NotFound title={t("notFound.monitor.title")} body={t("notFound.monitor.body")} back={{ href: "/", label: t("notFound.home") }} />
  );
}
