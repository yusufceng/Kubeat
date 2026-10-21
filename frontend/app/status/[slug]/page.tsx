import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { connection } from "next/server";
import { cache, Suspense } from "react";
import { ApiError, getStatusPageView, type StatusPageLocked, type StatusPageView } from "@/lib/api";
import { config } from "@/lib/config";
import { I18nProvider } from "@/lib/i18n/client";
import { getT } from "@/lib/i18n/server";
import { translator } from "@/lib/i18n/translate";
import { wallScript } from "@/lib/status-page";
import { RefreshCountdown } from "@/app/ui/refresh-countdown";
import { SystemTheme } from "@/app/ui/theme-toggle";
import { statusPagePath, statusPageToken } from "./access";
import { allMonitors as monitorsOf, Footer, Skeleton, Unavailable } from "./shared";
import { UnlockForm, unlockProblem } from "./unlock-form";
import { Announcement, Hero, Incidents, Maintenance, Sections, Updates } from "./view";
import { WallToggle, WallWatch } from "./wall";

/** The page's view, read once per request for both the page and its title, with the token the visitor holds for it. */
const readView = cache(async (slug: string) => getStatusPageView(slug, await statusPageToken(slug)));

/** Whether an error is a protected page's answer without its token. */
function locked(err: unknown): StatusPageLocked | null {
  if (err instanceof ApiError && err.status === 401 && typeof err.body === "object" && err.body !== null && (err.body as StatusPageLocked).locked) {
    return err.body as StatusPageLocked;
  }
  return null;
}

/** A published page is titled after itself, not after Kubeat; one that asks to stay out of search engines says so. */
export async function generateMetadata({ params }: PageProps<"/status/[slug]">): Promise<Metadata> {
  try {
    const view = await readView((await params).slug);
    return {
      title: { absolute: view.title },
      description: view.description || undefined,
      ...(view.noindex && { robots: { index: false } }),
    };
  } catch (err) {
    // A page behind a password shows its title, and is nobody's to index.
    const lock = locked(err);
    return lock ? { title: { absolute: lock.title }, robots: { index: false } } : {}; // the page itself says what is wrong
  }
}

/** When the request is answered: the page is read from the backend each time it is asked for, and "now" is then. */
async function requestTime(): Promise<number> {
  await connection();
  return Date.now();
}

async function View({
  params,
  searchParams,
}: {
  params: Promise<{ slug: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const now = await requestTime();
  const { slug } = await params;
  // How often the page reads itself again: a setting of the running server.
  const tools = (
    <div className="sp-head-tools">
      <RefreshCountdown intervalMs={config.statusPageRefreshMs} />
      <WallToggle />
    </div>
  );

  let view: StatusPageView;
  try {
    view = await readView(slug);
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) notFound();
    const visitor = await getT();
    const lock = locked(err);
    if (lock) {
      const query = await searchParams;
      return <UnlockForm slug={slug} title={lock.title} next={statusPagePath(slug)} problem={unlockProblem(query.unlock)} t={visitor} />;
    }
    return <Unavailable t={visitor} tools={tools} />;
  }

  // The page speaks the language its editor chose, whatever the visitor's
  // browser asks for; the client components under it follow.
  const t = translator(view.language);
  const monitors = monitorsOf(view);
  return (
    <I18nProvider locale={view.language}>
      {/* The document's lang is the visitor's until the browser takes over; the page says its own here. */}
      <div lang={view.language} className="contents">
      <WallWatch state={view.state} down={monitors.filter((m) => m.state === "down").map((m) => m.id)} />
      <header className="sp-head">
        <div className="sp-head-main">
          <h1 className="sp-title">{view.title}</h1>
          {view.description && <p className="sp-desc">{view.description}</p>}
        </div>
        {tools}
      </header>

      {view.announcement && <Announcement text={view.announcement} t={t} />}

      <Hero view={view} t={t} />

      <Sections view={view} t={t} compactAbove={config.statusPageCompactAbove} />

      <Maintenance view={view} t={t} now={now} />

      <div className="sp-wall-grid">
        <Updates view={view} t={t} />
        <Incidents view={view} t={t} />
      </div>
      <Footer t={t} />
      </div>
    </I18nProvider>
  );
}

/**
 * A published status page, for anyone: its own plain frame, without the
 * app's navigation or anything about who is signed in, in the system's
 * light or dark. A visitor who turned it into a wall screen gets that
 * layout before the first paint.
 */
export default async function Page({ params, searchParams }: PageProps<"/status/[slug]">) {
  const t = await getT();
  return (
    <main className="sp-page">
      <script dangerouslySetInnerHTML={{ __html: wallScript }} />
      <SystemTheme />
      <Suspense
        fallback={
          <>
            <Skeleton label={t("common.loading")} />
            <Footer t={t} />
          </>
        }
      >
        <View params={params} searchParams={searchParams} />
      </Suspense>
    </main>
  );
}
