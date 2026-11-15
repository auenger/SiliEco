import { getStaticDownloadRelease } from "@/features/landing/utils/static-downloads";
import { DownloadClient } from "./download-client";

export const dynamic = "force-dynamic";

export default function DownloadPage() {
  return <DownloadClient release={getStaticDownloadRelease()} />;
}
