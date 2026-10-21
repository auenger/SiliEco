import { test, expect } from "@playwright/test";
import { createTestApi } from "./helpers";

test("skill candidates preserve live content and use consistent responsive dialogs", async ({ page }, testInfo) => {
  test.setTimeout(300000);
  test.skip(!process.env.DATABASE_URL, "Candidate fixture cleanup requires acceptance database");
  const api = await createTestApi();
  let skillId = "";
  try {
    const skill = await api.createSkillCandidateFixture(`revision-browser-${Date.now()}`);
    skillId = skill.id;
    const token = api.getToken();
    if (!token) throw new Error("Missing login");
    await page.addInitScript((token) => { localStorage.setItem("silieco_token", token); localStorage.setItem("silieco:chat:isOpen", "false"); }, token);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`/${api.getWorkspaceSlug()}/skills/${skillId}?view=revisions`, { waitUntil: "domcontentloaded" });
    await page.getByRole("tab", { name: "Versions", exact: true }).click();
    await expect(page.getByText("No revisions yet.")).toBeVisible();
    await page.getByRole("button", { name: "Create candidate", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByLabel("Saved skill content")).toHaveValue("# Saved baseline");
    await dialog.getByLabel("Candidate content").fill("# Browser candidate");
    await dialog.getByLabel("Compatible contract versions", { exact: false }).fill("sample/v1");
    await dialog.getByRole("button", { name: "Add tool", exact: true }).click();
    await dialog.getByLabel("Tool name").fill("node");
    await dialog.getByLabel("Exact version").fill("22.1.0");
    const createWidth = (await dialog.boundingBox())!.width;
    expect(createWidth).toBeGreaterThan(800);
    await page.screenshot({ path: testInfo.outputPath("candidate-desktop.png"), animations: "disabled" });
    await page.setViewportSize({ width: 390, height: 844 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await expect.poll(async () => (await dialog.boundingBox())!.width).toBeLessThanOrEqual(358);
    await page.screenshot({ path: testInfo.outputPath("candidate-mobile.png"), animations: "disabled" });
    await page.setViewportSize({ width: 1440, height: 1000 });
    const response = page.waitForResponse((r) => r.url().endsWith(`/api/skills/${skillId}/revisions`) && r.request().method() === "POST");
    await dialog.getByRole("button", { name: "Create candidate", exact: true }).click();
    const created = await response;
    expect(created.status(), await created.text()).toBe(201);
    expect(created.request().postDataJSON()).toMatchObject({ content: "# Browser candidate", files: [{ path: "references/example.md", content: "Saved supporting file" }], compatible_contracts: ["sample/v1"], tool_dependencies: [{ name: "node", version: "22.1.0" }] });
    await expect(dialog.getByRole("heading", { name: "View revision" })).toBeVisible();
    await expect(dialog.getByRole("textbox", { name: "SKILL.md", exact: true })).toHaveValue("# Browser candidate");
    await expect.poll(async () => (await dialog.boundingBox())!.width).toBe(createWidth);
    await page.screenshot({ path: testInfo.outputPath("revision-desktop.png"), animations: "disabled" });
    expect((await api.readSkillCandidateFixture(skillId)).content).toBe("# Saved baseline");
    await dialog.getByRole("button", { name: "Close", exact: true }).first().click();
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("tab", { name: "Versions", exact: true }).click();
    await page.getByRole("button", { name: "View revision", exact: true }).click();
    await expect(page.getByRole("dialog").getByRole("textbox", { name: "SKILL.md", exact: true })).toHaveValue("# Browser candidate");
  } finally {
    if (skillId) await api.deleteSkillCandidateFixture(skillId);
    await api.cleanup();
  }
});
