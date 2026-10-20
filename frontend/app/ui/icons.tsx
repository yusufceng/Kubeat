import type { SVGProps } from "react";

/** A 16px stroke icon; the paths are drawn on a 24px grid. */
function Icon({ children, ...props }: SVGProps<SVGSVGElement>) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
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
    <circle cx="11" cy="11" r="7" />
    <path d="m20 20-3.5-3.5" />
  </Icon>
);

export const PauseIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M9 5v14M15 5v14" />
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
    <path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z" />
  </Icon>
);

export const DisplayIcon = (p: Props) => (
  <Icon {...p}>
    <rect x="3" y="4" width="18" height="12" rx="2" />
    <path d="M8 20h8M12 16v4" />
  </Icon>
);

/** The mark of the product: a pulse. */
export const PulseIcon = (p: Props) => (
  <Icon {...p}>
    <path d="M2 12h4l3-8 5 16 3-8h5" />
  </Icon>
);
