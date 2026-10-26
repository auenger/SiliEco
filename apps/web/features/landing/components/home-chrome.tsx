"use client";
import { useEffect, useRef, useState } from "react";
import { Menu, ArrowUpRight } from "lucide-react";
import { SiliecoIcon } from "@silieco/ui/components/common/silieco-icon";
import { useLocale, locales, localeLabels } from "../i18n";

const labels = {
  "zh-Hans": {
    ecosystem: "协作生态",
    workflow: "如何协作",
    film: "协作短片",
    contact: "联系我们",
    dark: "切换到暗色主题",
    light: "切换到亮色主题",
  },
  en: {
    ecosystem: "Ecosystem",
    workflow: "How it works",
    film: "Watch the film",
    contact: "Contact",
    dark: "Switch to dark theme",
    light: "Switch to light theme",
  },
  ja: {
    ecosystem: "協働の仕組み",
    workflow: "進め方",
    film: "紹介動画",
    contact: "お問い合わせ",
    dark: "ダークテーマに切り替え",
    light: "ライトテーマに切り替え",
  },
  ko: {
    ecosystem: "협업 생태계",
    workflow: "협업 방식",
    film: "소개 영상",
    contact: "문의",
    dark: "어두운 테마로 전환",
    light: "밝은 테마로 전환",
  },
};
export function HomeHeader({
  theme,
  onToggleTheme,
}: {
  theme: "light" | "dark";
  onToggleTheme: () => void;
}) {
  const { t, locale } = useLocale();
  const c = labels[locale];
  const menu = useRef<HTMLDetailsElement>(null);
  const [turning, setTurning] = useState(false);
  useEffect(() => {
    if (!turning) return;
    const timer = window.setTimeout(() => setTurning(false), 520);
    return () => window.clearTimeout(timer);
  }, [turning, theme]);
  const links = [
    { href: "#ecosystem", label: c.ecosystem },
    { href: "#how-it-works", label: c.workflow },
    { href: "#film", label: c.film },
    { href: "#contact", label: c.contact },
  ];
  return (
    <header className="home-header">
      <div className="home-shell home-header-inner">
        <a href="#product" className="home-brand">
          <SiliecoIcon className="size-8" noSpin />
          <span>
            Silieco<small>HUMAN × AGENT</small>
          </span>
        </a>
        <nav className="home-desktop-nav" aria-label={t.header.navigation}>
          {links.map((link) => (
            <a key={link.href} href={link.href}>
              {link.label}
            </a>
          ))}
        </nav>
        <button
          className={`home-theme-toggle${turning ? " is-turning" : ""}`}
          type="button"
          aria-label={theme === "light" ? c.dark : c.light}
          aria-pressed={theme === "dark"}
          onClick={() => {
            setTurning(true);
            onToggleTheme();
          }}
        >
          <span className="home-theme-corner" aria-hidden="true" />
          <span className="home-theme-fold" aria-hidden="true" />
          <span className="home-theme-hint" aria-hidden="true">
            {theme === "light" ? c.dark : c.light}
          </span>
        </button>
        <details
          className="home-mobile-menu"
          ref={menu}
          onKeyDown={(event) => {
            if (event.key === "Escape" && menu.current) {
              menu.current.open = false;
              menu.current.querySelector("summary")?.focus();
            }
          }}
        >
          <summary aria-label={t.header.openMenu}>
            <Menu size={18} />
          </summary>
          <nav aria-label={t.header.navigation}>
            {links.map((link) => (
              <a
                key={link.href}
                href={link.href}
                onClick={() => {
                  if (menu.current) menu.current.open = false;
                }}
              >
                {link.label}
              </a>
            ))}
          </nav>
        </details>
      </div>
    </header>
  );
}
export function HomeFooter() {
  const { t, locale, setLocale } = useLocale();
  return (
    <footer className="home-footer home-shell">
      <div className="home-footer-bottom">
        <p>
          {t.footer.copyright.replace(
            "{year}",
            String(new Date().getFullYear()),
          )}
        </p>
        <a href="https://siliroot.com/" target="_blank" rel="noreferrer">
          siliroot.com <ArrowUpRight size={12} />
        </a>
        <div>
          {locales.map((l) => (
            <button
              key={l}
              aria-pressed={locale === l}
              onClick={() => setLocale(l)}
            >
              {localeLabels[l]}
            </button>
          ))}
        </div>
      </div>
    </footer>
  );
}
