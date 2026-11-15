// Quote only values requiring shell escaping, keeping ordinary URLs readable.
export function shellArgument(value: string): string {
  return /^[a-zA-Z0-9_./:@-]+$/.test(value) ? value : "'" + value.replaceAll("'", "'\"'\"'") + "'";
}
export function cliCommands(serverUrl?: string, appUrl?: string) {
  const server = serverUrl?.trim().replace(/\/+$/, "") ?? "";
  const app = appUrl?.trim().replace(/\/+$/, "") ?? "";
  const base = `${app || "https://silieco.ai"}/cli`;
  return {
    installCmd: `curl -fsSL ${shellArgument(base + "/install.sh")} | SILIECO_DOWNLOAD_BASE_URL=${shellArgument(base)} bash`,
    setupCmd: server && app ? `silieco setup self-host --server-url ${shellArgument(server)} --app-url ${shellArgument(app)}` : "silieco setup",
  };
}
