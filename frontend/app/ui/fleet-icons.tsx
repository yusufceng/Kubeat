import type { SVGProps } from "react";

/**
 * The few icons the overview, the monitor list and the command palette add
 * to app/ui/icons.tsx, drawn the same way: a 24px grid, a 1.5px line and
 * round ends, 16px by default.
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

/** Leads on to a page: "Open", "All incidents". */
export const ArrowIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M5 12h14M13 6l6 6-6 6" />
  </Icon>
);

/** Filters that are not on view: two sliders. */
export const FilterIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M4 7.5h9M17 7.5h3M4 16.5h3M11 16.5h9" />
    <circle cx="15" cy="7.5" r="2" />
    <circle cx="9" cy="16.5" r="2" />
  </Icon>
);

/** A certificate close to its end: a shield. */
export const CertIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M12 3.5 19 6v5.6c0 4.2-2.9 7.4-7 8.9-4.1-1.5-7-4.7-7-8.9V6Z" />
    <path d="M12 8.5v4M12 15.6v.1" />
  </Icon>
);

export const CloseIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m6.5 6.5 11 11M17.5 6.5l-11 11" />
  </Icon>
);

export const CheckIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m5 12.5 4.5 4.5L19 7.5" />
  </Icon>
);

/** A namespace: a box drawn in dashes, as a boundary rather than a thing. */
export const NamespaceIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="3.5" y="5" width="17" height="14" rx="2" strokeDasharray="3 2.4" />
    <path d="M8 10h8M8 14h5" />
  </Icon>
);
