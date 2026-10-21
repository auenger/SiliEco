import type { Metadata } from "next";
import { getStaticDownloadRelease } from "@/features/landing/utils/static-downloads";
import { DownloadClient } from "./download-client";

export const metadata: Metadata = {
  title: "下载 SiliEco",
  description: "下载 SiliEco macOS 与 Windows 桌面端。",
  openGraph: {
    title: "下载 SiliEco",
    description: "下载 SiliEco macOS 与 Windows 桌面端。",
    url: "/download",
  },
  alternates: {
    canonical: "/download",
  },
};

export default async function DownloadPage() {
  const release = getStaticDownloadRelease();
  return <DownloadClient release={release} />;
}
