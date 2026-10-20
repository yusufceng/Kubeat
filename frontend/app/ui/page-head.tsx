/**
 * The head of a section page: its name in the display face, a sentence on
 * what the page is for, and on the right what can be done from it (the
 * children).
 */
export function PageHead({
  title,
  intro,
  children,
}: {
  title: string;
  intro?: React.ReactNode;
  children?: React.ReactNode;
}) {
  return (
    <header className="pg-head">
      <div className="pg-head-main">
        <h1 className="pg-title">{title}</h1>
        {intro && <p className="pg-intro">{intro}</p>}
      </div>
      {children && <div className="pg-head-actions">{children}</div>}
    </header>
  );
}
