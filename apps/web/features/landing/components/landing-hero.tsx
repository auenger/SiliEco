"use client";

import Link from "next/link";
import { ArrowDownRight, ArrowRight, Download, Radio } from "lucide-react";
import { useAuthStore } from "@silieco/core/auth";
import { useLocale } from "../i18n";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import {
  ClaudeCodeLogo,
  CodexLogo,
  GeminiCliLogo,
  OpenClawLogo,
  OpenCodeLogo,
  heroButtonClassName,
} from "./shared";

export function LandingHero() {
  const { t } = useLocale();
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();

  return (
    <div className="relative min-h-full overflow-hidden bg-[#070b12] text-white">
      <LandingBackdrop />

      <main className="relative z-10">
        <section
          id="product"
          className="mx-auto max-w-[1320px] px-4 pb-16 pt-28 sm:px-6 sm:pt-32 lg:px-8 lg:pb-24 lg:pt-40"
        >
          <div className="grid items-end gap-12 lg:grid-cols-[minmax(0,1.35fr)_minmax(330px,0.65fr)] lg:gap-16">
            <div>
              <div className="inline-flex items-center gap-2 border border-[#3978f6]/34 bg-[#3978f6]/10 px-3 py-1.5 font-mono text-micro font-semibold tracking-[0.16em] text-[#8db4ff]">
                <Radio className="size-3.5" aria-hidden />
                {t.hero.eyebrow}
              </div>
              <h1 className="mt-7 max-w-[920px] text-[3.65rem] font-semibold leading-[0.91] tracking-[-0.055em] text-white sm:text-[4.9rem] lg:text-[6.25rem]">
                {t.hero.headlineLine1}
                <br />
                <span className="text-white/48">{t.hero.headlineLine2}</span>
              </h1>

              <p className="mt-7 max-w-[760px] text-body-lg leading-8 text-white/68 sm:text-title">
                {t.hero.subheading}
              </p>

              <div className="mt-9 flex flex-wrap items-center gap-3">
                <Link href={ctaHref} className={heroButtonClassName("solid")}>
                  {user ? t.header.dashboard : t.hero.cta}
                  <ArrowRight className="size-4" aria-hidden />
                </Link>
                <Link href="/download" className={heroButtonClassName("ghost")}>
                  <Download className="size-4" aria-hidden />
                  {t.hero.downloadDesktop}
                </Link>
                <Link
                  href="#architecture"
                  className="group inline-flex items-center justify-center gap-1.5 px-3 py-3 text-body font-semibold text-white/70 transition-colors hover:text-white"
                >
                  {t.hero.talkToSales}
                  <ArrowDownRight
                    className="size-4 transition-transform group-hover:translate-x-0.5 group-hover:translate-y-0.5"
                    aria-hidden
                  />
                </Link>
              </div>
            </div>

            <div className="border border-white/12 bg-black/18 p-5 backdrop-blur-md sm:p-6">
              <div className="flex items-center justify-between border-b border-white/10 pb-4">
                <span className="font-mono text-micro font-semibold tracking-[0.15em] text-white/42">
                  SILIECO SYSTEM
                </span>
                <span className="flex items-center gap-2 text-micro text-[#8db4ff]">
                  <span className="size-1.5 rounded-full bg-[#3978f6] shadow-[0_0_12px_#3978f6]" />
                  ONLINE
                </span>
              </div>
              <div className="mt-2">
                {t.system.layers.map((layer, index) => (
                  <div
                    key={layer.name}
                    className="grid grid-cols-[38px_72px_1fr] items-center gap-3 border-b border-white/8 py-4 last:border-0"
                  >
                    <span className="font-mono text-micro text-white/28">
                      {String(index + 1).padStart(2, "0")}
                    </span>
                    <span className="font-mono text-label font-semibold tracking-[0.12em] text-white">
                      {layer.name}
                    </span>
                    <span className="text-caption text-white/48">
                      {layer.label}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </div>

          <div className="mt-14 flex flex-col gap-5 border-t border-white/10 pt-6 lg:flex-row lg:items-center lg:justify-between">
            <span className="font-mono text-micro font-semibold uppercase tracking-[0.14em] text-white/36">
              {t.hero.worksWith}
            </span>
            <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
              <RuntimeLogo logo={<ClaudeCodeLogo className="size-4" />} label="Claude Code" />
              <RuntimeLogo logo={<CodexLogo className="size-4" />} label="Codex" />
              <RuntimeLogo logo={<GeminiCliLogo className="size-4" />} label="Gemini CLI" />
              <RuntimeLogo logo={<OpenClawLogo className="size-4" />} label="OpenClaw" />
              <RuntimeLogo logo={<OpenCodeLogo className="size-4" />} label="OpenCode" />
            </div>
          </div>

          <div id="preview" className="mt-8 sm:mt-10">
            <ProductImage alt={t.hero.imageAlt} />
          </div>
        </section>
      </main>
    </div>
  );
}

function LandingBackdrop() {
  return (
    <div className="pointer-events-none absolute inset-0">
      <div className="absolute inset-0 [background-image:linear-gradient(to_right,rgba(255,255,255,0.045)_1px,transparent_1px),linear-gradient(to_bottom,rgba(255,255,255,0.045)_1px,transparent_1px)] [background-size:56px_56px]" />
      <div className="absolute -left-40 top-[-10%] size-[560px] rounded-full bg-[#3978f6]/20 blur-[140px]" />
      <div className="absolute right-[-12%] top-[20%] size-[520px] rounded-full bg-[#164691]/16 blur-[160px]" />
    </div>
  );
}

const PREVIEW_COLUMNS = [
  {
    label: "Backlog",
    count: 3,
    accent: "bg-white/30",
    tasks: [
      ["SILI-24", "Map the next product release"],
      ["SILI-31", "Review workspace permissions"],
    ],
  },
  {
    label: "In progress",
    count: 2,
    accent: "bg-[#f3b743]",
    tasks: [
      ["SILI-18", "Build the runtime health view"],
      ["SILI-27", "Connect the release daemon"],
    ],
  },
  {
    label: "Agent review",
    count: 2,
    accent: "bg-[#5a8dff]",
    tasks: [
      ["SILI-12", "Prepare onboarding content"],
      ["SILI-29", "Validate the Core API"],
    ],
  },
  {
    label: "Done",
    count: 4,
    accent: "bg-[#45c486]",
    tasks: [
      ["SILI-08", "Create the Silieco workspace"],
      ["SILI-16", "Register Sili Agent"],
    ],
  },
] as const;

function ProductImage({ alt }: { alt: string }) {
  return (
    <div className="relative" role="img" aria-label={alt}>
      <div className="absolute -inset-px bg-gradient-to-r from-[#3978f6]/70 via-white/12 to-transparent opacity-70" />
      <div className="relative overflow-hidden border border-white/10 bg-[#0a0e15] p-1.5 shadow-[0_36px_120px_rgba(0,0,0,0.42)]">
        <div className="overflow-hidden border border-white/8 bg-[#f5f6f8] text-[#111827]">
          <div className="flex h-11 items-center justify-between border-b border-black/8 bg-white px-3 sm:px-4">
            <div className="flex items-center gap-2.5">
              <span className="grid size-6 grid-cols-2 gap-0.5 rounded-md bg-[#08111f] p-1.5">
                <span className="bg-[#3978f6]" />
                <span className="bg-white" />
                <span className="bg-white" />
                <span className="bg-[#3978f6]" />
              </span>
              <span className="text-caption font-semibold sm:text-body">
                Silieco Workspace
              </span>
              <span className="hidden font-mono text-micro text-black/38 sm:inline">
                / OPERATIONS
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="hidden rounded-full border border-black/10 px-2.5 py-1 text-micro text-black/55 sm:inline">
                Sili Agent online
              </span>
              <span className="grid size-7 place-items-center rounded-full bg-[#3978f6] text-micro font-bold text-white">
                SA
              </span>
            </div>
          </div>

          <div className="flex min-h-[390px] sm:min-h-[470px]">
            <aside className="hidden w-[180px] shrink-0 border-r border-black/8 bg-white p-3 sm:block">
              <div className="mb-5 px-2 font-mono text-micro font-semibold tracking-[0.16em] text-black/35">
                WORKSPACE
              </div>
              {["Inbox", "My tasks", "Board", "Agents", "Runtimes"].map(
                (item) => (
                  <div
                    key={item}
                    className={`mb-1 rounded-md px-2.5 py-2 text-caption ${
                      item === "Board"
                        ? "bg-[#eaf0ff] font-semibold text-[#245dcc]"
                        : "text-black/54"
                    }`}
                  >
                    {item}
                  </div>
                ),
              )}
              <div className="mt-8 border-t border-black/8 pt-3">
                <div className="flex items-center gap-2 rounded-lg bg-[#08111f] p-2.5 text-white">
                  <span className="size-2 rounded-full bg-[#5a8dff] shadow-[0_0_8px_#5a8dff]" />
                  <div>
                    <div className="text-micro font-semibold">CORE</div>
                    <div className="font-mono text-micro text-white/48">
                      HEALTHY
                    </div>
                  </div>
                </div>
              </div>
            </aside>

            <div className="min-w-0 flex-1 p-3 sm:p-5">
              <div className="mb-5 flex items-end justify-between">
                <div>
                  <div className="font-mono text-micro font-semibold tracking-[0.14em] text-[#3978f6]">
                    LIVE WORK
                  </div>
                  <div className="mt-1 text-title font-semibold sm:text-display-xs">
                    Agent operations
                  </div>
                </div>
                <button className="rounded-md bg-[#08111f] px-3 py-2 text-micro font-semibold text-white">
                  + New task
                </button>
              </div>

              <div className="grid grid-cols-2 gap-2.5 lg:grid-cols-4">
                {PREVIEW_COLUMNS.map((column) => (
                  <div key={column.label} className="min-w-0">
                    <div className="mb-2 flex items-center justify-between px-1">
                      <span className="flex items-center gap-1.5 text-micro font-semibold text-black/58">
                        <span className={`size-1.5 rounded-full ${column.accent}`} />
                        {column.label}
                      </span>
                      <span className="font-mono text-micro text-black/30">
                        {column.count}
                      </span>
                    </div>
                    <div className="space-y-2">
                      {column.tasks.map(([id, title], taskIndex) => (
                        <div
                          key={id}
                          className="min-h-[112px] rounded-lg border border-black/8 bg-white p-3 shadow-[0_2px_8px_rgba(15,23,42,0.04)]"
                        >
                          <div className="font-mono text-micro font-medium text-black/35">
                            {id}
                          </div>
                          <div className="mt-2 text-caption font-semibold leading-5 text-black/76">
                            {title}
                          </div>
                          <div className="mt-4 flex items-center justify-between">
                            <span
                              className={`rounded px-1.5 py-0.5 text-micro font-semibold ${
                                taskIndex === 0
                                  ? "bg-[#eaf0ff] text-[#245dcc]"
                                  : "bg-black/[0.045] text-black/42"
                              }`}
                            >
                              {taskIndex === 0 ? "SILI AGENT" : "TEAM"}
                            </span>
                            <span className="size-4 rounded-full bg-gradient-to-br from-[#3978f6] to-[#08111f]" />
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function RuntimeLogo({
  logo,
  label,
}: {
  logo: React.ReactNode;
  label: string;
}) {
  return (
    <div className="flex items-center gap-2 text-white/62">
      {logo}
      <span className="text-label font-medium">{label}</span>
    </div>
  );
}
