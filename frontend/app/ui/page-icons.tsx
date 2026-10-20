import type { SVGProps } from "react";

/**
 * Icons the section pages (status pages, notifications, maintenance,
 * discovery, users, the audit log, sign-in) add to the shared set in
 * app/ui/icons.tsx, drawn the same way: a 24px grid, a 1.5px line with
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

/** Opens something elsewhere: a published page in a new tab, a monitor. */
export const OpenIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M13.5 4.5H19.5V10.5M19.5 4.5 11 13" />
    <path d="M18 14v4a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4" />
  </Icon>
);

/** Sends a test: a paper plane. */
export const SendIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M20.5 3.5 10 14" />
    <path d="M20.5 3.5 14 20.5l-4-6.5-6.5-4Z" />
  </Icon>
);

/** Ends something that is going on: a square. */
export const StopIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="6" y="6" width="12" height="12" rx="2" />
  </Icon>
);

export const CheckIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m5 12.5 4.5 4.5L19 7.5" />
  </Icon>
);

export const CloseIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m6.5 6.5 11 11M17.5 6.5l-11 11" />
  </Icon>
);

/** Something to take note of before going on: a triangle with a mark. */
export const WarningIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M10.3 4.9a2 2 0 0 1 3.4 0l7.1 12.3a2 2 0 0 1-1.7 3H4.9a2 2 0 0 1-1.7-3Z" />
    <path d="M12 10v3.5M12 16.6v.1" />
  </Icon>
);

/** A notice to readers: a megaphone. */
export const AnnounceIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M4 10v4a1 1 0 0 0 1 1h2l6 4V5L7 9H5a1 1 0 0 0-1 1Z" />
    <path d="M16.5 9a4 4 0 0 1 0 6M7.5 15l1 4.5" />
  </Icon>
);

/** What only some may change: a padlock. */
export const LockIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="5" y="10.5" width="14" height="10" rx="2" />
    <path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5" />
  </Icon>
);

/** Signs in through a provider: a door with an arrow going in. */
export const SignInIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M10 4.5h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-7M4 12h10M10.5 8.5 14 12l-3.5 3.5" />
  </Icon>
);

/** A link that could not be followed: the backend out of reach. */
export const UnplugIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m4 20 3-3M20 4l-3 3M9.5 9.5 7 12a3.5 3.5 0 0 0 5 5l2.5-2.5M14.5 14.5 17 12a3.5 3.5 0 0 0-5-5L9.5 9.5" />
    <path d="M4 4l16 16" />
  </Icon>
);

/** A period on the calendar: a maintenance window. */
export const CalendarIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="3.5" y="5" width="17" height="15.5" rx="2" />
    <path d="M3.5 10h17M8 3v4M16 3v4" />
  </Icon>
);

/** A record kept: a log page with lines. */
export const LogIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M7 3.5h8l4 4v13H7Z" />
    <path d="M10 11h6M10 14.5h6M10 18h3" />
  </Icon>
);
