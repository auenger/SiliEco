import {
  File, FileText, FileCode2, FileJson2, FileImage, FileAudio, FileVideo,
  FileArchive, FileSpreadsheet, Presentation, FileType2, FileTerminal,
  Settings2, Database, Braces, GitBranch, Globe,
} from "lucide-react";

/** Shared, statically imported file glyphs; unknown formats retain a file icon. */
export function fileTypeIcon(name: string) {
  const filename = name.split("/").pop()?.toLowerCase() ?? "";
  const ext = filename.split(".").pop() ?? "";
  if (/^(\.git|\.gitignore|\.gitattributes|\.gitmodules)$/.test(filename)) return GitBranch;
  if (/^(dockerfile|makefile|\.env.*|.*\.config\..*)$/.test(filename)) return Settings2;
  if (["md", "mdx", "txt", "pdf", "doc", "docx", "rtf", "odt"].includes(ext)) return FileText;
  if (["html", "htm"].includes(ext)) return Globe;
  if (["json", "jsonc", "jsonl", "ipynb"].includes(ext)) return FileJson2;
  if (["yaml", "yml", "toml", "ini", "conf", "lock"].includes(ext)) return Settings2;
  if (["png", "jpg", "jpeg", "gif", "webp", "svg", "ico", "bmp", "avif", "heic", "tiff"].includes(ext)) return FileImage;
  if (["mp3", "wav", "ogg", "m4a", "flac", "aac"].includes(ext)) return FileAudio;
  if (["mp4", "mov", "webm", "avi", "mkv"].includes(ext)) return FileVideo;
  if (["zip", "gz", "bz2", "xz", "tar", "tgz", "7z", "rar"].includes(ext)) return FileArchive;
  if (["xls", "xlsx", "csv", "tsv", "ods", "numbers"].includes(ext)) return FileSpreadsheet;
  if (["ppt", "pptx", "key", "odp"].includes(ext)) return Presentation;
  if (["woff", "woff2", "ttf", "otf"].includes(ext)) return FileType2;
  if (["sh", "bash", "zsh", "ps1", "bat", "cmd"].includes(ext)) return FileTerminal;
  if (["sql", "db", "sqlite", "sqlite3"].includes(ext)) return Database;
  if (["css", "scss", "sass", "less"].includes(ext)) return Braces;
  if (["js", "jsx", "ts", "tsx", "mjs", "cjs", "py", "go", "rs", "java", "c", "cpp", "h", "hpp", "cs", "rb", "php", "swift", "kt", "vue", "svelte", "xml"].includes(ext)) return FileCode2;
  return File;
}

export function FileTypeIcon({ name, className }: { name: string; className?: string }) {
  const Icon = fileTypeIcon(name);
  return <Icon className={className} aria-hidden="true" />;
}
