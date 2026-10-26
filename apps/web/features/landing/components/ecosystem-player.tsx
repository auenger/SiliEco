"use client";
import { useCallback, useRef, useState, useSyncExternalStore } from "react";
import { Player } from "@remotion/player";
import type { PlayerRef } from "@remotion/player";
import { Pause, Play } from "lucide-react";
import { EcosystemScene } from "./ecosystem-scene";

const compactQuery = "(max-width: 900px)";
const subscribe = (listener: () => void) => {
  const query = window.matchMedia(compactQuery);
  query.addEventListener("change", listener);
  return () => query.removeEventListener("change", listener);
};
const snapshot = () => window.matchMedia(compactQuery).matches;
const serverSnapshot = () => false;

export function EcosystemPlayer({
  reduced,
  light,
  playLabel,
  pauseLabel,
}: {
  reduced: boolean;
  light: boolean;
  playLabel: string;
  pauseLabel: string;
}) {
  const compact = useSyncExternalStore(subscribe, snapshot, serverSnapshot);
  const ref = useRef<PlayerRef>(null);
  const [paused, setPaused] = useState(false);
  const observeContainer = useCallback(
    (element: HTMLDivElement | null) => {
      if (!element) return;
      let visible = false;
      const sync = () => {
        if (visible && !reduced && !paused && !document.hidden)
          ref.current?.play();
        else ref.current?.pause();
      };
      const observer = new IntersectionObserver(
        ([entry]) => {
          visible = !!entry?.isIntersecting;
          sync();
        },
        { threshold: 0.15 },
      );
      observer.observe(element);
      document.addEventListener("visibilitychange", sync);
      return () => {
        observer.disconnect();
        document.removeEventListener("visibilitychange", sync);
        ref.current?.pause();
      };
    },
    [reduced, paused],
  );
  return (
    <div key={String(compact)} className="ecosystem-player" ref={observeContainer}>
      <Player
        key={String(compact)}
        ref={ref}
        component={EcosystemScene}
        inputProps={{ compact, light }}
        durationInFrames={2640}
        fps={30}
        compositionWidth={compact ? 760 : 1920}
        compositionHeight={compact ? 1510 : 680}
        loop
        autoPlay={!reduced}
        controls={false}
        clickToPlay={false}
        numberOfSharedAudioTags={0}
        style={{ width: "100%" }}
        errorFallback={() => (
          <p className="ecosystem-error">
            生态动画暂时无法加载，请观看下方完整影片。
          </p>
        )}
      />
      <div className="ecosystem-caption">
        <span>HUMAN + AGENT / 工作 SOP 协作生态</span>
        {!reduced && (
          <button
            type="button"
            onClick={() => setPaused(!paused)}
            aria-label={paused ? playLabel : pauseLabel}
          >
            {paused ? <Play size={14} /> : <Pause size={14} />}
            <span>{paused ? playLabel : pauseLabel}</span>
          </button>
        )}
      </div>
    </div>
  );
}
