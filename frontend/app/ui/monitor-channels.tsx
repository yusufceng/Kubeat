import Link from "next/link";
import { getMonitorChannels, type MonitorChannel } from "@/lib/api";
import { channelEventLabels, channelTypes } from "@/lib/channel-types";
import { getT } from "@/lib/i18n/server";

/**
 * Where a monitor's alerts go, so that a monitor nobody would hear about
 * is noticed before it fails.
 */
export async function MonitorChannels({ monitorId, canEdit }: { monitorId: number; canEdit: boolean }) {
  let channels: MonitorChannel[];
  try {
    channels = await getMonitorChannels(monitorId);
  } catch {
    return null; // the page itself reports a backend that is away
  }
  const t = await getT();
  if (channels.length === 0) {
    return (
      <p className="mt-1.5 text-sm text-warn">
        {t("monitorPage.channels.none")}
        {canEdit && (
          <>
            {" "}
            <Link href="/channels" className="underline">
              {t("monitorPage.channels.add")}
            </Link>
            .
          </>
        )}
      </p>
    );
  }
  const describe = (c: MonitorChannel) =>
    c.events.length === 0 ? "" : ` (${c.events.map((e) => t(channelEventLabels[e]).toLowerCase()).join(", ")})`;
  return (
    <p className="mt-1.5 text-sm text-muted">
      {t("monitorPage.channels.label")}{" "}
      {channels.map((c, i) => (
        <span key={c.id}>
          {i > 0 && ", "}
          <span className="text-ink">{c.name}</span>
          <span className="text-xs"> {t(channelTypes[c.type]?.label ?? c.type)}</span>
          {describe(c)}
        </span>
      ))}
    </p>
  );
}
