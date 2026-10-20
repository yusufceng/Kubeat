import { getT } from "@/lib/i18n/server";
import { NotFound } from "@/app/ui/empty-state";

/**
 * An incident that is not there, or not for this viewer (the backend
 * answers both alike), told inside the app's frame.
 */
export default async function IncidentNotFound() {
  const t = await getT();
  return (
    <main className="page">
      <NotFound
        title={t("incident.notFound.title")}
        body={t("incident.notFound.body")}
        back={{ href: "/incidents", label: t("incident.notFound.back") }}
      />
    </main>
  );
}
