/**
 * The mark: a heptagon, after the wheel of Kubernetes, holding three beats
 * (short, tall, mid). The same drawing as app/icon.svg, here in the
 * theme's accent so that it follows light and dark.
 */
export function LogoMark({ size = 28, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden className={className}>
      <path
        fill="var(--accent)"
        stroke="var(--accent)"
        strokeWidth="1.6"
        strokeLinejoin="round"
        d="M16 2.545 27.102 7.891 29.844 19.905 22.161 29.539H9.839L2.156 19.905 4.898 7.891Z"
      />
      <path
        fill="none"
        stroke="var(--accent-ink)"
        strokeWidth="2.6"
        strokeLinecap="round"
        d="M11 15.15v3.2M16 11.55v10.4M21 13.75v6"
      />
    </svg>
  );
}

/** The name as it is drawn: lowercase Mona Sans, widened to 112.5 and bold. */
export function Wordmark({ className = "" }: { className?: string }) {
  return <span className={`font-bold tracking-[-0.02em] [font-stretch:112.5%] ${className}`}>kubeat</span>;
}

/** The mark and the name together, where the product introduces itself. */
export function Logo({ size = 28, className = "" }: { size?: number; className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2.5 ${className}`}>
      <LogoMark size={size} />
      <Wordmark />
    </span>
  );
}
