import { test, expect } from "@playwright/test";
import { createTestApi } from "./helpers";

test("SOP contract configuration keeps drafts local and publishes the selected execution mode", async ({ page }, testInfo) => {
  test.setTimeout(180000);
  const api = await createTestApi();
  try {
    const slug = api.getWorkspaceSlug();
    const token = api.getToken();
    if (!token) throw new Error("Test login did not return a token");
    await page.addInitScript((token) => {
      localStorage.setItem("silieco_token", token);
      localStorage.setItem("silieco:chat:isOpen", "false");
    }, token);
    await page.goto(`/${slug}/workflows`, {waitUntil: "domcontentloaded"});
    await page.getByRole("button", {name: /空白 SOP/}).click();
    const name = `Contract acceptance ${Date.now()}`;
    await page.getByLabel("SOP name", {exact: true}).fill(name);
    await page.getByPlaceholder("Stage name", {exact: true}).fill("Delivery");
    await page.getByRole("radio", {name: /^Agent/}).check();
    await page.getByRole("button", {name: "Configure inputs and outputs"}).click();
    const dialog = page.getByRole("dialog", {name: "Configure inputs and outputs"});
    await expect(dialog).toBeVisible();
    await dialog.getByLabel("New field name").fill("summary");
    await dialog.getByRole("button", {name: "Add field", exact: true}).click();
    await dialog.getByRole("checkbox", {name: "Required", exact: true}).check();
    await page.setViewportSize({width: 1440, height: 1000});
    const desktop = await dialog.boundingBox();
    expect(desktop?.width).toBeGreaterThan(800);
    expect(desktop?.width).toBeLessThanOrEqual(900);
    await page.screenshot({path: testInfo.outputPath("contract-desktop.png"), fullPage: true});
    await page.setViewportSize({width: 390, height: 844});
    const mobile = await dialog.boundingBox();
    expect(mobile!.x).toBeGreaterThanOrEqual(0);
    expect(mobile!.x + mobile!.width).toBeLessThanOrEqual(390);
    expect(await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true);
    const scroll = dialog.locator('[data-slot="contract-scroll"]');
    const scrollBounds = await scroll.boundingBox();
    const applyBounds = await dialog.getByRole("button", {name: "Apply configuration"}).boundingBox();
    expect(scrollBounds!.y + scrollBounds!.height).toBeLessThanOrEqual(applyBounds!.y);
    await scroll.evaluate((node) => { node.scrollTop = node.scrollHeight; });
    await expect(dialog.getByRole("button", {name: "Add delivery"})).toBeInViewport();
    await page.screenshot({path: testInfo.outputPath("contract-mobile.png"), fullPage: true});
    await dialog.getByRole("button", {name: "Apply configuration"}).click();
    await expect(dialog).not.toBeVisible();
    await page.setViewportSize({width: 1440, height: 1000});
    await page.getByRole("radio", {name: /^Manual/}).check();
    const response = page.waitForResponse((response) => response.url().endsWith("/api/workflows") && response.request().method() === "POST");
    await page.getByRole("button", {name: "Publish SOP", exact: true}).click();
    const saved = await response;
    expect(saved.status(), await saved.text()).toBe(201);
    const workflow = await saved.json();
    api.trackCreatedWorkflow(workflow.id);
    const body = saved.request().postDataJSON();
    expect(body.stages[0]).toMatchObject({execution_mode: "manual", executor_id: null, output_spec: {contract: {data: {properties: {summary: {type: "string"}}, required: ["summary"]}}}});
    await expect(page.getByRole("button", {name: "Publish SOP", exact: true})).not.toBeVisible();
  } finally { await api.cleanup(); }
});
