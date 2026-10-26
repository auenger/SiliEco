"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { AnimatePresence, motion, MotionConfig, useInView } from "motion/react";
import {
  ArrowUpRight,
  ArrowRight,
  Check,
  Play,
  FileText,
  Pause,
  Bot,
  UserRound,
  GitBranch,
  FolderOpen,
} from "lucide-react";
import { HomeHeader, HomeFooter } from "./home-chrome";
import { EcosystemPlayer } from "./ecosystem-player";
import { useLocale, toLandingDictionaryLocale } from "../i18n";
import { homeCopy } from "../i18n/home-copy";
import "./silieco-home.css";

const reducedMotionQuery = "(prefers-reduced-motion: reduce)";
function subscribeReducedMotion(onChange: () => void) {
  const query = window.matchMedia(reducedMotionQuery);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}
const getReducedMotion = () => window.matchMedia(reducedMotionQuery).matches;
const getServerReducedMotion = () => false;

export function SiliecoLanding() {
  const { locale, t } = useLocale();
  const c = homeCopy[toLandingDictionaryLocale(locale)];
  const [theme, setTheme] = useState<"light" | "dark">("light");
  const reduced = useSyncExternalStore(
    subscribeReducedMotion,
    getReducedMotion,
    getServerReducedMotion,
  );
  const [step, setStep] = useState(0);
  const [autoPlay, setAutoPlay] = useState(true);
  const demo = useRef<HTMLDivElement>(null);
  const inView = useInView(demo, { amount: 0.6 });
  useEffect(() => {
    if (!autoPlay || reduced || !inView) return;
    const timer = window.setInterval(() => {
      if (!document.hidden)
        setStep((current) => (current + 1) % c.steps.length);
    }, 5500);
    return () => window.clearInterval(timer);
  }, [autoPlay, reduced, inView, c.steps.length]);
  const chooseStep = (index: number) => {
    setAutoPlay(false);
    setStep(index);
  };
  const [videoError, setVideoError] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const scene = c.steps[step]!;
  const openFilm = () => {
    video.current?.scrollIntoView({
      behavior: reduced ? "instant" : "smooth",
      block: "center",
    });
    video.current?.focus({ preventScroll: true });
    void video.current?.play().catch(() => {
      /* Native controls remain available when playback is blocked. */
    });
  };

  return (
    <MotionConfig
      reducedMotion={reduced ? "always" : "never"}
      transition={{ duration: 0.55, ease: [0.22, 1, 0.36, 1] }}
    >
      <div className="silieco-home" data-theme={theme}>
        <a className="home-skip" href="#product">
          {c.skip}
        </a>
        <HomeHeader
          theme={theme}
          onToggleTheme={() => setTheme(theme === "light" ? "dark" : "light")}
        />
        <main id="product">
          <section className="home-hero home-shell" id="ecosystem">
            <div className="hero-message">
              <p className="home-eyebrow">SILIECO / HUMAN × AGENT</p>
              <h1>
                {c.title[0]}
                <br />
                <span>{c.title[1]}</span>
              </h1>
              <p className="home-lead">{c.description}</p>
              <div className="home-actions">
                <a className="home-button" href="#how-it-works">
                  {c.useCases}
                  <ArrowUpRight size={18} />
                </a>
                <button className="home-film-link" onClick={openFilm}>
                  <Play size={16} />
                  {c.watch}
                  <span>76s</span>
                </button>
              </div>
            </div>
            <EcosystemPlayer
              reduced={reduced}
              light={theme === "light"}
              playLabel={c.autoPlay}
              pauseLabel={c.pause}
            />
          </section>
          <div className="home-shell compatibility">
            <span>{c.compatibility}</span>
            <div>
              <strong>Claude Code</strong>
              <strong>Codex</strong>
              <strong>OpenCode</strong>
              <strong>Gemini CLI</strong>
            </div>
          </div>

          <section className="home-story" id="how-it-works">
            <div className="home-shell story-layout">
              <div className="story-heading">
                <h2>
                  {c.workflowTitle[0]}
                  <br />
                  <span>{c.workflowTitle[1]}</span>
                </h2>
                <p>{c.workflowDescription}</p>
                <a href="#film" className="home-text-link">
                  {c.watch}
                  <ArrowUpRight size={18} />
                </a>
              </div>
              <div className="workflow-demo" ref={demo} data-ui>
                <div className="demo-top">
                  <span>
                    <GitBranch size={16} /> {c.demoTitle}
                  </span>
                  <span>{c.demoLabel}</span>
                </div>
                <div className="workflow-tabs" aria-label={c.chooseStep}>
                  {c.steps.map((item, index) => (
                    <button
                      key={item.title}
                      type="button"
                      aria-pressed={step === index}
                      onClick={() => chooseStep(index)}
                    >
                      <span
                        className={
                          step >= index ? "step-dot reached" : "step-dot"
                        }
                      >
                        {step > index ? <Check size={13} /> : index + 1}
                      </span>
                      {item.title}
                    </button>
                  ))}
                </div>
                <div
                  className="workflow-stage"
                  aria-live={autoPlay && !reduced ? "off" : "polite"}
                >
                  <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                      key={step}
                      initial={{ opacity: 0, y: reduced ? 0 : 14 }}
                      animate={{ opacity: 1, y: 0 }}
                      exit={{ opacity: 0, y: reduced ? 0 : -8 }}
                      transition={{ duration: 0.25 }}
                    >
                      <div className="demo-assignee">
                        <span className="demo-avatar">
                          {step === 1 ? <Bot /> : <UserRound />}
                        </span>
                        <div>
                          <strong>{scene.owner}</strong>
                          <span>{scene.role}</span>
                        </div>
                        <span className="demo-status">{scene.status}</span>
                      </div>
                      <h3>{scene.heading}</h3>
                      <p>{scene.description}</p>
                      <div className="demo-document">
                        <FileText size={22} />
                        <div>
                          <strong>{scene.file}</strong>
                          <span>{scene.detail}</span>
                        </div>
                        <Check size={17} />
                      </div>
                      <div className="demo-comment">
                        <span>↳</span>
                        <p>{scene.message}</p>
                      </div>
                    </motion.div>
                  </AnimatePresence>
                </div>
                <div className="demo-bottom">
                  <span>{c.stepCount.replace("{n}", String(step + 1))}</span>
                  {!reduced && (
                    <button
                      className="demo-playback"
                      onClick={() => setAutoPlay(!autoPlay)}
                      aria-label={autoPlay ? c.pause : c.autoPlay}
                      title={autoPlay ? c.pause : c.autoPlay}
                    >
                      {autoPlay ? <Pause size={15} /> : <Play size={15} />}
                    </button>
                  )}
                  <button
                    onClick={() => chooseStep((step + 1) % c.steps.length)}
                  >
                    {step === c.steps.length - 1 ? c.restart : c.next}
                    <ArrowRight size={16} />
                  </button>
                </div>
              </div>
            </div>
          </section>

          <section id="film" className="home-shell home-film-section">
            <div className="section-intro">
              <div>
                <h2>
                  {c.filmTitle[0]}
                  <br />
                  {c.filmTitle[1]}
                </h2>
              </div>
              <p>{c.filmDescription}</p>
            </div>
            <div className="film-frame">
              <video
                ref={video}
                controls
                playsInline
                preload="none"
                poster="/media/silieco-collaboration-v3.png"
                tabIndex={0}
                aria-label={c.videoLabel}
                onError={() => setVideoError(true)}
              >
                <source
                  src="/media/silieco-collaboration-v3.mp4"
                  type="video/mp4"
                />
                <track
                  kind="subtitles"
                  src="/media/silieco-collaboration-v3.zh.vtt"
                  srcLang="zh"
                  label="中文"
                />
              </video>
            </div>
            <div className="film-caption">
              <span>{videoError ? c.videoError : c.filmCaption}</span>
            </div>
          </section>

          <section className="home-capabilities home-shell" id="capabilities">
            <div className="capabilities-heading">
              <h2>
                {c.capabilitiesTitle[0]}
                <br />
                <span>{c.capabilitiesTitle[1]}</span>
              </h2>
              <p>{c.capabilitiesDescription}</p>
            </div>
            <div className="capability-list">
              {c.features.map((feature, index) => {
                const Icon = [GitBranch, FolderOpen, Bot][index]!;
                return (
                  <article key={feature.title}>
                    <Icon size={26} strokeWidth={1.3} />
                    <div>
                      <h3>{feature.title}</h3>
                      <p>{feature.description}</p>
                    </div>
                    <ArrowUpRight className="feature-arrow" size={22} />
                  </article>
                );
              })}
            </div>
          </section>
          <section className="home-shell home-architecture" id="architecture">
            <h2>{t.system.headline}</h2>
            <p>{t.system.description}</p>
            <div>
              {t.system.layers.map((layer) => (
                <article key={layer.name}>
                  <h3>
                    {layer.name} <span>{layer.label}</span>
                  </h3>
                  <p>{layer.description}</p>
                </article>
              ))}
            </div>
          </section>
          <section className="home-open" id="contact">
            <div className="home-shell">
              <div>
                <h2>{c.openTitle}</h2>
                <p>{c.openDescription}</p>
              </div>
              <div className="home-contact-links">
                <a className="home-button" href="mailto:machi@siliroot.com">
                  machi@siliroot.com
                  <ArrowUpRight size={18} />
                </a>
                <a
                  className="home-text-link"
                  href="https://siliroot.com/"
                  target="_blank"
                  rel="noreferrer"
                >
                  SiliRoot · 硅基源流
                  <ArrowUpRight size={16} />
                </a>
              </div>
            </div>
          </section>
        </main>
        <HomeFooter />
      </div>
    </MotionConfig>
  );
}
