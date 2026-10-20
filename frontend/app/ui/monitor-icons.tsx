import type { SVGProps } from "react";

/**
 * Icons the monitor page needs beyond the shared set (app/ui/icons.tsx),
 * drawn the same way: a 24px grid, a 1.5px line with round ends, 16px inline.
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

/** A bell struck through: notifications held back for a while. */
export const SilenceIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M6 16v-5a6 6 0 0 1 9.5-4.9M18 11v5l1.5 2H8M4 4l16 16" />
    <path d="M10 20.5a2.2 2.2 0 0 0 4 0" />
  </Icon>
);

export const ChevronDownIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m6 9 6 6 6-6" />
  </Icon>
);

export const CloseIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M6 6l12 12M18 6 6 18" />
  </Icon>
);

/** A magnifier with a plus: look closer at a span of time. */
export const ZoomInIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="11" cy="11" r="6.5" />
    <path d="m20 20-4.2-4.2M11 8.5v5M8.5 11h5" />
  </Icon>
);
