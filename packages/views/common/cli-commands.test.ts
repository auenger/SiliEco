import { expect, it } from "vitest";
import { cliCommands, shellArgument } from "./cli-commands";
it("uses the current self-host deployment for download and setup", () => {
 const commands = cliCommands("http://100.93.224.87:6688/", "http://100.93.224.87:6688/");
 expect(commands.installCmd).toContain("http://100.93.224.87:6688/cli/install.sh");
 expect(commands.installCmd).toContain("SILIECO_DOWNLOAD_BASE_URL=http://100.93.224.87:6688/cli");
 expect(commands.setupCmd).toBe("silieco setup self-host --server-url http://100.93.224.87:6688 --app-url http://100.93.224.87:6688");
 expect(JSON.stringify(commands)).not.toContain("yilia");
});
it("quotes shell metacharacters", () => {
 expect(shellArgument("https://host/a'$(id)")).toBe("'https://host/a'\"'\"'$(id)'");
});
