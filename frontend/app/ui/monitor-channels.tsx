import Link from "next/link";
import type { MonitorChannel } from "@/lib/api";
import { channelEventLabels, channelTypes } from "@/lib/channel-types";
import { duration } from "@/lib/format";
import type { T } from "@/lib/i18n/translate";
import { BellIcon } from "@/app/ui/icons";
import { Callout } from "@/app/ui/monitor-callout";

/** How many channel names the chip spells out before it counts the rest. */
const NAMED = 2;

/**
 * Where a monitor's alerts go, as a chip of the header's meta row: a bell
 * and the channels' names; each one's type, events and timing (a delay
 * before it hears of an outage, its own reminder cadence) are in its title.
 */
export function ChannelsChip({ channels, t }: { channels: MonitorChannel[]; t: T }) {
  const describe = (c: MonitorChannel) => {
    const events = c.events.map((e) => t(channelEventLabels[e]).toLocaleLowerCase(t.intl)).join(", ");
    const timing = [
      c.delay_seconds > 0 && t("channelRow.delay", { duration: duration(c.delay_seconds, t) }),
      c.repeat_seconds > 0 && t("channelRow.repeat", { duration: duration(c.repeat_seconds, t) }),
    ].filter(Boolean);
    const detail = [events, ...timing].filter(Boolean).join(" · ");
    return `${c.name} (${t(channelTypes[c.type]?.label ?? c.type)}${detail ? `: ${detail}` : ""})`;
  };
  const names = channels.slice(0, NAMED).map((c) => c.name);
  const rest = channels.length - names.length;
  return (
    <span className="mon-chip" title={t("monitorPage.channels.chip", { list: channels.map(describe).join(", ") })}>
      <BellIcon width={13} height={13} />
      <span>
        <span className="sr-only">{t("monitorPage.channels.label")} </span>
        {names.join(", ")}
        {rest > 0 && <span className="text-ink-3"> +{rest}</span>}
      </span>
    </span>
  );
}

/** A monitor nobody would hear about, said before it fails; editors get the way to fix it. */
export function NoChannelsCallout({ canEdit, t }: { canEdit: boolean; t: T }) {
  return (
    <Callout tone="warn" action={canEdit && <Link href="/channels">{t("monitorPage.channels.add")}</Link>}>
      {t("monitorPage.channels.none")}
    </Callout>
  );
}
