import type { SVGProps } from "react";

/**
 * Icons the incident and report pages need beyond the shared set
 * (app/ui/icons.tsx), drawn the same way: a 24px grid, a 1.5px line with
 * round ends, 16px inline.
 */
function Icon({ children, ...props }: SVGProps<SVGSVGElement>) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      {...props}
    >
      {children}
    </svg>
  );
}

type Props = SVGProps<SVGSVGElement>;

export const DownloadIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M12 4v11M7.5 10.5 12 15l4.5-4.5M5 19.5h14" />
  </Icon>
);

/** A line chart: to see a span of time on the monitor's chart. */
export const ChartIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M4 4v16h16" />
    <path d="m7.5 14.5 3.5-4 3 2.5 5-6" />
  </Icon>
);

/** An arrow out of a box: to the page of what is named. */
export const OpenIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M13.5 5H19v5.5M19 5l-8 8" />
    <path d="M17 14v4a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 5 18V8.5A1.5 1.5 0 0 1 6.5 7H10" />
  </Icon>
);

/** A sheet with a fold and lines: a note. */
export const NoteIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M5 4.5h14v10l-5 5H5Z" />
    <path d="M14 19.5v-5h5M8.5 9h7M8.5 12.5h4" />
  </Icon>
);

/** A bell that rings again: a reminder. */
export const ReminderIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M7 16v-5a5 5 0 0 1 10 0v5l1.5 2h-13Z" />
    <path d="M10.2 20.5a2 2 0 0 0 3.6 0M3.5 8.5a8.5 8.5 0 0 1 2.5-4.5M20.5 8.5a8.5 8.5 0 0 0-2.5-4.5" />
  </Icon>
);

/** The wheel's heptagon, outlined, with its hub: something the cluster said. */
export const ClusterIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M12 3.2 18.9 6.5l1.7 7.5-4.8 6H8.2l-4.8-6 1.7-7.5Z" />
    <circle cx="12" cy="12.4" r="2.2" fill="currentColor" stroke="none" />
  </Icon>
);

/** A flag on a pole: where the incident was opened. */
export const FlagIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M6 20.5V4" />
    <path d="M6 4.5h11l-2.5 4 2.5 4H6" />
  </Icon>
);

/** Three dots in a row: entries left out. */
export const GapIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M6 12h.01M12 12h.01M18 12h.01" strokeWidth="2.5" />
  </Icon>
);

export const CheckIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m5 12.5 4.5 4.5L19 7.5" />
  </Icon>
);

export const CrossIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M6.5 6.5l11 11M17.5 6.5l-11 11" />
  </Icon>
);

export const ChevronDownIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m6 9 6 6 6-6" />
  </Icon>
);

/** Two arrows, up and down: a column that can be sorted. */
export const SortIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m8 9.5 4-4 4 4M8 14.5l4 4 4-4" />
  </Icon>
);

/** One arrow: the direction a column is sorted in. */
export const ArrowUpIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M12 19V5M6.5 10.5 12 5l5.5 5.5" />
  </Icon>
);

export const ArrowDownIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M12 5v14M6.5 13.5 12 19l5.5-5.5" />
  </Icon>
);

export const CloseIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M6 6l12 12M18 6 6 18" />
  </Icon>
);

/** Three lines narrowing down: what narrows a list. */
export const FilterIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M4 6.5h16M7 12h10M10 17.5h4" />
  </Icon>
);

/** A calendar page: dates of one's own. */
export const CalendarIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="4" y="5.5" width="16" height="14.5" rx="2" />
    <path d="M4 10h16M8.5 3.5v4M15.5 3.5v4" />
  </Icon>
);
