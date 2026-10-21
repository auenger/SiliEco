import { test, expect } from "@playwright/test";
import { createTestApi } from "./helpers";

test("manual review rejects an old Run state and reopens a fresh review", async ({page}, testInfo) => {
  test.setTimeout(180000);
  const api = await createTestApi();
  try {
    const run = await api.createManualWorkflowRun(`Manual review ${Date.now()}`);
    const token = api.getToken();
    if (!token) throw new Error("Missing test login");
    await page.addInitScript((token) => {
      localStorage.setItem("silieco_token",token);
      localStorage.setItem("silieco:chat:isOpen","false");
    },token);
    await page.setViewportSize({width:1440,height:1000});
    await page.goto(`/${api.getWorkspaceSlug()}/projects/${run.projectId}?run=${run.runId}`,{waitUntil:"domcontentloaded"});
    await page.getByRole("button",{name:"Review Stage",exact:true}).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("button",{name:"Approve and complete this run"})).toBeEnabled();
    const desktop = await dialog.boundingBox();
    expect(desktop!.width).toBeGreaterThan(600);
    expect(desktop!.width).toBeLessThanOrEqual(680);
    await page.screenshot({path:testInfo.outputPath("manual-review-desktop.png"),fullPage:true});
    // A second client's rejection changes the state while retaining this Stage.
    const changed = await api.transitionWorkflowRun(run.runId,run.stateToken,"rejected");
    expect(changed.status,await changed.text()).toBe(200);
    const stale = await api.transitionWorkflowRun(run.runId,run.stateToken,"approved");
    expect(stale.status,await stale.text()).toBe(409);
    await expect(dialog.getByRole("alert")).toContainText("The Run changed");
    await expect(dialog.getByRole("button",{name:"Approve and complete this run"})).toBeDisabled();
    await page.keyboard.press("Escape");
    await page.getByRole("button",{name:"Review Stage",exact:true}).click();
    await expect(dialog.getByRole("button",{name:"Approve and complete this run"})).toBeEnabled();
    await page.setViewportSize({width:390,height:844});
    const mobile = await dialog.boundingBox();
    expect(mobile!.x).toBeGreaterThanOrEqual(0);
    expect(mobile!.x+mobile!.width).toBeLessThanOrEqual(390);
    expect(await dialog.evaluate((node)=>node.scrollWidth<=node.clientWidth)).toBe(true);
    await expect(dialog.getByRole("button",{name:"Approve and complete this run"})).toBeInViewport();
    await page.screenshot({path:testInfo.outputPath("manual-review-mobile.png"),fullPage:true});
    const response = page.waitForResponse((r)=>r.url().endsWith(`/api/workflow-instances/${run.runId}/transition`) && r.request().method()==="POST");
    await dialog.getByRole("button",{name:"Approve and complete this run"}).click();
    const approved = await response;
    expect(approved.status(),await approved.text()).toBe(200);
    expect((await approved.json()).status).toBe("completed");
    await expect(dialog).not.toBeVisible();
  } finally { await api.cleanup(); }
});
