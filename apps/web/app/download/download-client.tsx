"use client";

import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import type { LatestRelease } from "@/features/landing/utils/github-release";
import { detectOS, type DetectResult } from "@/features/landing/utils/os-detect";
import { AppleIcon, WindowsIcon } from "@/features/landing/components/download/os-icons";

export function DownloadClient({ release }: { release: LatestRelease }) {
  const [detected, setDetected] = useState<DetectResult | null>(null);

  useEffect(() => {
    let cancelled = false;
    detectOS().then((result) => {
      if (!cancelled) setDetected(result);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <main className="min-h-screen bg-[#080a0f] text-[#f2f4f7]">
      <header className="mx-auto flex h-[78px] w-[min(1120px,calc(100%-40px))] items-center justify-between border-b border-white/10">
        <a href="/" className="flex items-center gap-2.5" aria-label="SiliEco 首页">
          <img src="/product-home/siliroot-mark.svg" alt="" className="h-8 w-8 invert" />
          <img src="/product-home/siliroot-wordmark.svg" alt="SiliRoot" className="h-8 w-[100px] invert" />
        </a>
        <span className="font-mono text-micro uppercase tracking-[0.18em] text-[#78a7ff]">SiliEco / Downloads</span>
      </header>

      <section className="mx-auto w-[min(1120px,calc(100%-40px))] pb-20 pt-20 sm:pb-28 sm:pt-28">
        <p className="font-mono text-micro uppercase tracking-[0.18em] text-[#78a7ff]">下载 SiliEco</p>
        <div className="mt-6 flex flex-col justify-between gap-8 border-b border-white/10 pb-12 lg:flex-row lg:items-end">
          <div>
            <h1 className="max-w-[720px] text-5xl font-semibold leading-[.95] tracking-[-.07em] sm:text-7xl">把真实工作，<br /><span className="text-[#78a7ff]">带到桌面。</span></h1>
            <p className="mt-7 max-w-[520px] text-title-sm leading-8 text-[#a6adba]">下载桌面端，在同一个工作系统里连接任务、Agent、SOP 与验证记录。</p>
          </div>
          {release.version ? <span className="w-fit rounded-full border border-white/15 px-3 py-1.5 font-mono text-micro text-[#a6adba]">INTERNAL PREVIEW · {release.version}</span> : null}
        </div>

        <div className="mt-10 grid gap-3 md:grid-cols-3">
          <DownloadCard icon={<AppleIcon className="text-[#f2f4f7]" />} title="macOS Apple Silicon" note={detected?.os === "mac" && detected.arch === "arm64" ? "推荐用于当前设备" : "适用于 M 系列芯片"} primary={{ label: "下载 DMG", href: release.assets.macArm64Dmg }} secondary={{ label: "ZIP", href: release.assets.macArm64Zip }} />
          <DownloadCard icon={<AppleIcon className="text-[#f2f4f7]" />} title="macOS Intel" note={detected?.os === "mac" && detected.arch === "x64" ? "推荐用于当前设备" : "适用于 Intel 芯片"} primary={{ label: "下载 DMG", href: release.assets.macX64Dmg }} secondary={{ label: "ZIP", href: release.assets.macX64Zip }} />
          <DownloadCard icon={<WindowsIcon className="text-[#f2f4f7]" />} title="Windows x64" note="便携版，解压后即可运行" primary={{ label: "下载 ZIP", href: release.assets.winX64Zip }} />
        </div>

        <p className="mt-8 font-mono text-micro leading-6 text-[#667080]">内测版本仅提供 macOS 与 Windows 桌面端。下载后按系统提示完成首次启动。</p>
      </section>
    </main>
  );
}

function DownloadCard({ icon, title, note, primary, secondary }: { icon: ReactNode; title: string; note: string; primary: { label: string; href?: string }; secondary?: { label: string; href?: string } }) {
  return (
    <article className="group flex min-h-[260px] flex-col rounded-2xl border border-white/12 bg-white/[.035] p-6 transition-colors hover:border-[#78a7ff]/60 hover:bg-white/[.06]">
      <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-white/[.08]">{icon}</div>
      <h2 className="mt-12 text-title-lg font-semibold tracking-[-.04em]">{title}</h2>
      <p className="mt-2 text-body leading-6 text-[#a6adba]">{note}</p>
      <div className="mt-auto flex flex-wrap gap-2 pt-7"><DownloadLink {...primary} prominent />{secondary ? <DownloadLink {...secondary} /> : null}</div>
    </article>
  );
}

function DownloadLink({ label, href, prominent = false }: { label: string; href?: string; prominent?: boolean }) {
  if (!href) return <span className="rounded-full border border-white/10 px-4 py-2 text-caption text-white/30">暂不可用</span>;
  return <a href={href} download className={prominent ? "rounded-full bg-[#f2f4f7] px-4 py-2 text-caption font-semibold text-[#080a0f] transition-transform hover:-translate-y-0.5" : "rounded-full border border-white/20 px-4 py-2 text-caption font-semibold text-[#f2f4f7] transition-colors hover:border-white/60"}>{label}</a>;
}
