"use client";

import { useEffect, useRef, useState, type ComponentType } from "react";
import {
  File,
  FileArchive,
  FileAudio,
  FileCode2,
  FileImage,
  FileSpreadsheet,
  FileText,
  FileVideo,
  Presentation,
} from "lucide-react";
import type { Attachment } from "@silieco/core/types";
import { resolvePublicFileUrl } from "@silieco/core/workspace/avatar-url";
import { getPreviewKind } from "../editor/utils/preview";

type FileVisualSource = Pick<
  Attachment,
  "content_type" | "download_url" | "filename" | "markdown_url" | "url"
>;

const ARCHIVE_EXTENSIONS = new Set(["7z", "bz2", "gz", "rar", "tar", "tgz", "zip"]);
const CODE_EXTENSIONS = new Set([
  "c", "cpp", "css", "go", "h", "html", "java", "js", "json", "jsx",
  "kt", "md", "php", "py", "rb", "rs", "sh", "sql", "swift", "toml",
  "ts", "tsx", "vue", "xml", "yaml", "yml",
]);
const DOCUMENT_EXTENSIONS = new Set(["doc", "docx", "odt", "pdf", "rtf", "txt"]);
const PRESENTATION_EXTENSIONS = new Set(["key", "odp", "ppt", "pptx"]);
const SPREADSHEET_EXTENSIONS = new Set(["csv", "numbers", "ods", "xls", "xlsx"]);

function extensionOf(filename: string): string {
  const dot = filename.lastIndexOf(".");
  return dot >= 0 ? filename.slice(dot + 1).toLowerCase() : "";
}

function thumbnailUrlOf(file: FileVisualSource): string | null {
  const rawUrl = file.download_url || file.markdown_url || file.url;
  if (!rawUrl) return null;
  if (/^(?:https?:|blob:|data:)/i.test(rawUrl)) return rawUrl;
  return resolvePublicFileUrl(rawUrl);
}

function getFileResourceIcon(file: Pick<Attachment, "content_type" | "filename">): ComponentType<{ className?: string }> {
  const kind = getPreviewKind(file.content_type, file.filename);
  if (kind === "image") return FileImage;
  if (kind === "video") return FileVideo;
  if (kind === "audio") return FileAudio;

  const contentType = file.content_type.toLowerCase();
  const extension = extensionOf(file.filename);
  if (contentType.includes("zip") || contentType.includes("compressed") || ARCHIVE_EXTENSIONS.has(extension)) {
    return FileArchive;
  }
  if (contentType.includes("spreadsheet") || contentType.includes("excel") || SPREADSHEET_EXTENSIONS.has(extension)) {
    return FileSpreadsheet;
  }
  if (contentType.includes("presentation") || contentType.includes("powerpoint") || PRESENTATION_EXTENSIONS.has(extension)) {
    return Presentation;
  }
  if (
    contentType.includes("json") ||
    contentType.includes("javascript") ||
    contentType.includes("xml") ||
    CODE_EXTENSIONS.has(extension)
  ) {
    return FileCode2;
  }
  if (kind === "pdf" || kind === "text" || kind === "markdown" || DOCUMENT_EXTENSIONS.has(extension)) {
    return FileText;
  }
  return File;
}

export function FileResourceLeadingVisual({
  file,
  compact = false,
}: {
  file: FileVisualSource;
  compact?: boolean;
}) {
  const kind = getPreviewKind(file.content_type, file.filename);
  const Icon = getFileResourceIcon(file);
  const imageUrl = kind === "image" ? thumbnailUrlOf(file) : null;
  const ref = useRef<HTMLSpanElement>(null);
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!imageUrl || visible) return;
    if (typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        setVisible(true);
        observer.disconnect();
      }
    });
    if (ref.current) observer.observe(ref.current);
    return () => observer.disconnect();
  }, [imageUrl, visible]);


  return (
    <span
      ref={ref}
      className={`relative flex shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted/60 ${compact ? "size-7" : "size-9"}`}
      aria-hidden="true"
      data-file-visual={kind === "image" ? "thumbnail" : "icon"}
    >
      <Icon className={`${compact ? "size-3.5" : "size-4"} text-muted-foreground`} />
      {imageUrl && visible ? (
        <img
          src={imageUrl}
          alt=""
          className="absolute inset-0 block size-full object-contain object-center p-0.5"
          loading="lazy"
          decoding="async"
          onError={(event) => { event.currentTarget.hidden = true; }}
        />
      ) : null}
    </span>
  );
}
