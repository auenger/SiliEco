/** Keep tagless source builds recognizable to the existing development gates. */
export function cliBuildVersion(description, commitCount) {
  const hash = /^([0-9a-f]{7,40})(-dirty)?$/i.exec(description);
  if (!hash) return description || "dev";
  if (!/^\d+$/.test(commitCount)) return description;
  // 0.0.0 is a synthetic baseline, not a claimed release or repository tag.
  return `v0.0.0-${commitCount}-g${hash[1]}${hash[2] ?? ""}`;
}
