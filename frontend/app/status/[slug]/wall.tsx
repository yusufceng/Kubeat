"use client";

import { useEffect, useRef, useSyncExternalStore } from "react";
import { useT } from "@/lib/i18n/client";
import { newlyDown, type PageState, WALL_KEY, worsened } from "@/lib/status-page";
import { DisplayIcon } from "@/app/ui/icons";

/*
 * The wall screen: the page as a screen on an ops room's wall shows it.
 * Bigger words, no footer, the whole screen, the page still refreshing
 * itself, and a short tone when a service goes down or the page's state
 * gets worse. The choice is kept in the browser (localStorage); the sound
 * can only start from a click, which is the browser's rule for audio, so a
 * reopened wall screen asks for one.
 */

type Wall = { on: boolean; sound: boolean; fullscreen: boolean };

let state: Wall = { on: false, sound: false, fullscreen: false };
const off: Wall = { on: false, sound: false, fullscreen: false };
const listeners = new Set<() => void>();

function set(change: Partial<Wall>) {
  state = { ...state, ...change };
  listeners.forEach((notify) => notify());
}
function subscribe(onChange: () => void) {
  listeners.add(onChange);
  return () => {
    listeners.delete(onChange);
  };
}

/** Marks the document, which the stylesheet reads (html[data-wall="on"]). */
function apply(on: boolean) {
  if (on) document.documentElement.dataset.wall = "on";
  else delete document.documentElement.dataset.wall;
}

function stored(): boolean {
  try {
    return localStorage.getItem(WALL_KEY) === "on";
  } catch {
    return false;
  }
}
function store(on: boolean) {
  try {
    if (on) localStorage.setItem(WALL_KEY, "on");
    else localStorage.removeItem(WALL_KEY);
  } catch {
    // Without storage the choice lasts until the page is left.
  }
}

// One context for the page, made inside a click: a context made elsewhere
// starts suspended and stays silent until resumed from one.
let audio: AudioContext | null = null;

function armSound() {
  if (typeof AudioContext === "undefined") return;
  if (!audio) audio = new AudioContext();
  if (audio.state === "suspended") void audio.resume();
  set({ sound: true });
}

/** Two short notes, the second lower: something got worse. */
function playTone() {
  if (!audio || !state.sound) return;
  const start = audio.currentTime;
  [880, 660].forEach((hz, i) => {
    const at = start + i * 0.24;
    const osc = audio!.createOscillator();
    const gain = audio!.createGain();
    osc.type = "sine";
    osc.frequency.setValueAtTime(hz, at);
    // Ramps in and out, so that the note does not click.
    gain.gain.setValueAtTime(0.0001, at);
    gain.gain.exponentialRampToValueAtTime(0.2, at + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, at + 0.2);
    osc.connect(gain).connect(audio!.destination);
    osc.start(at);
    osc.stop(at + 0.22);
  });
}

function enterFullscreen() {
  const el = document.documentElement;
  if (!el.requestFullscreen) return;
  el.requestFullscreen().catch(() => {
    // Refused (no gesture, or an iframe without the permission): the page still wears the wall layout.
  });
}

/**
 * The toggle in the page's head, and beside it while the wall screen is
 * on: the sound, and the way back to the whole screen when the visitor
 * has left it (Escape leaves fullscreen but not the wall screen).
 */
export function WallToggle() {
  const wall = useSyncExternalStore(subscribe, () => state, () => off);
  const t = useT();

  useEffect(() => {
    const on = stored();
    apply(on);
    set({ on, fullscreen: Boolean(document.fullscreenElement) });
    const onChange = () => set({ fullscreen: Boolean(document.fullscreenElement) });
    document.addEventListener("fullscreenchange", onChange);
    return () => document.removeEventListener("fullscreenchange", onChange);
  }, []);

  function enter() {
    store(true);
    apply(true);
    set({ on: true });
    armSound();
    enterFullscreen();
  }
  function leave() {
    store(false);
    apply(false);
    set({ on: false });
    if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
  }

  return (
    <div className="sp-wall-controls" role="group" aria-label={t("statusPage.wall.group")}>
      {wall.on && (
        <>
          <button type="button" className="btn btn-sm" aria-pressed={wall.sound} onClick={() => (wall.sound ? set({ sound: false }) : armSound())}>
            {wall.sound ? t("statusPage.wall.soundOn") : t("statusPage.wall.soundOff")}
          </button>
          {!wall.fullscreen && (
            <button type="button" className="btn btn-sm" onClick={enterFullscreen}>
              {t("statusPage.wall.fullscreen")}
            </button>
          )}
        </>
      )}
      <button type="button" className="btn btn-sm" aria-pressed={wall.on} onClick={wall.on ? leave : enter} title={t("statusPage.wall.title")}>
        <DisplayIcon width={14} height={14} />
        {t("statusPage.wall.toggle")}
      </button>
    </div>
  );
}

/**
 * Watches what each refresh brings: the page's state and which services
 * are down. When the state climbs towards down, or a service is down that
 * was not, the tone sounds (if the sound is on). The first render sets
 * the baseline: an outage already going on does not sound.
 */
export function WallWatch({ state: pageState, down }: { state: PageState; down: number[] }) {
  const key = down.join(",");
  const before = useRef<{ state: PageState; down: number[] } | null>(null);
  useEffect(() => {
    const ids = key ? key.split(",").map(Number) : [];
    const previous = before.current;
    before.current = { state: pageState, down: ids };
    if (!previous) return;
    if (worsened(previous.state, pageState) || newlyDown(previous.down, ids).length > 0) playTone();
  }, [pageState, key]);
  return null;
}
