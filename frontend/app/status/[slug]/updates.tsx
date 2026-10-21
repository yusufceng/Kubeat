"use client";

import type { StatusPageViewUpdate } from "@/lib/api";
import { useT } from "@/lib/i18n/client";
import { byDay, updateMarks } from "@/lib/status-page";
import { useMounted } from "@/lib/use-mounted";
import { Clock, DayLabel } from "@/app/ui/incident-time";
import { KindMark, kindLabel } from "@/app/ui/status-page-kind";

/**
 * What the page told its visitors, newest first, under the day each was
 * written on in the visitor's own zone (the backend groups by UTC; the
 * browser regroups once it knows the zone). Each update carries the glyph
 * of its kind and its time; an incident note flagged for the page shows
 * among them under the service's name.
 */
export function UpdatesTimeline({ updates }: { updates: StatusPageViewUpdate[] }) {
  const t = useT();
  const mounted = useMounted();
  const days = byDay(updates, mounted);
  return (
    <div className="panel sp-updates">
      {days.map((day) => (
        <div key={day.day} className="sp-updates-day">
          <h3 className="sp-day">
            <DayLabel iso={day.updates[0].created_at} />
          </h3>
          <ol>
            {day.updates.map((u, i) => (
              <li key={`${u.created_at}-${i}`} className="sp-update">
                <span className="sp-update-time">
                  <Clock iso={u.created_at} seconds={false} />
                </span>
                <span className={`sp-update-mark sp-update-mark-${updateMarks[u.kind]}`} aria-hidden>
                  <KindMark kind={u.kind} size={12} />
                </span>
                <div className="sp-update-body">
                  <p className="sp-update-title">
                    <b>{u.title}</b>
                    <span className="sp-update-kind">{t(kindLabel(u.kind))}</span>
                    {u.monitor_name && u.kind !== "note" && <span className="sp-update-monitor">{u.monitor_name}</span>}
                  </p>
                  {u.body && <p className="sp-update-text">{u.body}</p>}
                </div>
              </li>
            ))}
          </ol>
        </div>
      ))}
    </div>
  );
}
