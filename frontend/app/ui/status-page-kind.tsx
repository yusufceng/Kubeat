import type { StatusPageUpdateKind } from "@/lib/api";
import { updateMarks } from "@/lib/status-page";
import { NoteIcon } from "@/app/ui/incident-icons";
import { AnnounceIcon } from "@/app/ui/page-icons";
import { StatusGlyph } from "@/app/ui/status-glyph";

/** The message key of an update kind's name, for the editor and the public page alike. */
export const kindLabel = (kind: StatusPageUpdateKind | "note") => `statusPage.kind.${kind}`;

/**
 * How an update's kind is drawn: the glyph of the state it tells of
 * (investigating a diamond, resolved a circle, maintenance hatched), or an
 * icon where it is a plain word: a megaphone for a notice, a pen for an
 * incident note.
 */
export function KindMark({ kind, size = 10 }: { kind: StatusPageUpdateKind | "note"; size?: 10 | 12 | 16 }) {
  const mark = updateMarks[kind];
  if (mark === "notice") return <AnnounceIcon width={size + 4} height={size + 4} />;
  if (mark === "note") return <NoteIcon width={size + 4} height={size + 4} />;
  return <StatusGlyph kind={mark} size={size} />;
}
