import type { SVGProps } from "react";

/**
 * A stroke icon drawn on a 24px grid with a 1.5px line and round ends; 16px
 * inline, 20px in the rail (pass width and height).
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

export const PlusIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M12 5v14M5 12h14" />
  </Icon>
);

export const SearchIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="11" cy="11" r="6.5" />
    <path d="m20 20-4.2-4.2" />
  </Icon>
);

export const PauseIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M9 6v12M15 6v12" />
  </Icon>
);

export const PlayIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m7 5 12 7-12 7z" />
  </Icon>
);

export const EditIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M4 20h4L19 9l-4-4L4 16z" />
    <path d="m13.5 6.5 4 4" />
  </Icon>
);

export const CopyIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="9" y="9" width="11" height="11" rx="2" />
    <path d="M5 15V6a2 2 0 0 1 2-2h9" />
  </Icon>
);

export const RenewIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M21 12a9 9 0 1 1-9-9c2.5 0 4.9 1 6.7 2.7L21 8" />
    <path d="M21 3v5h-5" />
  </Icon>
);

export const TrashIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M4 7h16M10 11v6M14 11v6M6 7l1 12a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-12M9 7V4h6v3" />
  </Icon>
);

export const ChevronIcon = (p: Props) => (
  <Icon {...p}>
    <path d="m9 6 6 6-6 6" />
  </Icon>
);

export const SunIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </Icon>
);

export const MoonIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M19.5 14.5A8 8 0 0 1 9.5 4.5a8 8 0 1 0 10 10Z" />
  </Icon>
);

export const DisplayIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="3" y="4" width="18" height="12" rx="2" />
    <path d="M8 20h8M12 16v4" />
  </Icon>
);

/* The sections of the app, as the rail and the tab bar show them. */

/** The overview: the fleet as tiles, one of them beating. */
export const OverviewIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="3.5" y="3.5" width="7" height="7" rx="1.5" />
    <rect x="13.5" y="3.5" width="7" height="7" rx="1.5" />
    <rect x="3.5" y="13.5" width="7" height="7" rx="1.5" />
    <path d="M13.5 17h2l1.5-2.5 2 5 1.5-2.5" />
  </Icon>
);

export const IncidentsIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M10.3 4.9a2 2 0 0 1 3.4 0l7.1 12.3a2 2 0 0 1-1.7 3H4.9a2 2 0 0 1-1.7-3Z" />
    <path d="M12 10v3.5M12 16.6v.1" />
  </Icon>
);

export const MaintenanceIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M15.5 4.6a4.6 4.6 0 0 0-5.7 5.7L4.4 15.7a2.2 2.2 0 0 0 3.1 3.1l5.4-5.4a4.6 4.6 0 0 0 5.7-5.7l-2.6 2.6-2.4-.6-.6-2.4Z" />
  </Icon>
);

export const StatusPagesIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="2" />
    <path d="M7.8 7.8a6 6 0 0 0 0 8.4M16.2 7.8a6 6 0 0 1 0 8.4M5 5a10 10 0 0 0 0 14M19 5a10 10 0 0 1 0 14" />
  </Icon>
);

export const ReportsIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M4 20h16M7 16v-6M12 16V5M17 16v-4" />
  </Icon>
);

/** Discovery: a radar sweeping the cluster. */
export const DiscoveryIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 12 18 6M12 7.5a4.5 4.5 0 1 0 4.5 4.5" />
  </Icon>
);

export const BellIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M6 16v-5a6 6 0 0 1 12 0v5l1.5 2h-15Z" />
    <path d="M10 20.5a2.2 2.2 0 0 0 4 0" />
  </Icon>
);

export const UsersIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="9" cy="8.5" r="3.5" />
    <path d="M3 19.5a6 6 0 0 1 12 0M16 5.5a3.5 3.5 0 0 1 0 6.5M18 14.5a5.5 5.5 0 0 1 3 5" />
  </Icon>
);

export const AuditIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M7 3.5h8l4 4v13H7Z" />
    <path d="M10 11h6M10 14.5h6M10 18h3" />
  </Icon>
);

export const MoreIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="5.5" cy="12" r="0.8" />
    <circle cx="12" cy="12" r="0.8" />
    <circle cx="18.5" cy="12" r="0.8" />
  </Icon>
);

export const AccountIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="8.5" />
    <circle cx="12" cy="10" r="3" />
    <path d="M6.5 18.2a6.5 6.5 0 0 1 11 0" />
  </Icon>
);

export const SignOutIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M14 4.5H7a2 2 0 0 0-2 2v11a2 2 0 0 0 2 2h7M11 12h9M17 8.5l3.5 3.5-3.5 3.5" />
  </Icon>
);

export const LanguageIcon = (p: Props) => (
  <Icon {...p}>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M3.5 12h17M12 3.5c2.3 2.3 3.5 5.2 3.5 8.5s-1.2 6.2-3.5 8.5c-2.3-2.3-3.5-5.2-3.5-8.5S9.7 5.8 12 3.5Z" />
  </Icon>
);
