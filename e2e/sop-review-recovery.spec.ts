import { test, expect } from "@playwright/test";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { createTestApi } from "./helpers";

const execute = promisify(execFile);
for (const mode of ["seed", "missing"]) test(`${mode} agent review can be explicitly recovered without reusing its call`, async ({page}, testInfo) => {
  test.setTimeout(300000);
  test.skip(!process.env.DATABASE_URL,"Controlled review fixture requires the acceptance database");
  const api=await createTestApi();
  let runId="";
  const fixture=async(fixtureMode=mode)=>execute("go",["test","./internal/service","-run","^TestWorkflowReviewBrowserFixture$","-count=1"],{cwd:"core",env:{...process.env,SILIECO_REVIEW_BROWSER_RUN:runId,SILIECO_REVIEW_BROWSER_MODE:fixtureMode},timeout:120000});
  try {
    const run=await api.createManualWorkflowRun(`Review recovery browser ${Date.now()}`);runId=run.runId;
    await fixture();
    const token=api.getToken();if(!token)throw new Error("Missing test login");
    await page.addInitScript(token=>{localStorage.setItem("silieco_token",token);localStorage.setItem("silieco:chat:isOpen","false");},token);
    await page.setViewportSize({width:1440,height:1000});
    await page.goto(`/${api.getWorkspaceSlug()}/projects/${run.projectId}?run=${runId}`,{waitUntil:"domcontentloaded"});
    await page.getByRole("button",{name:"Recover review",exact:true}).click();
    const dialog=page.getByRole("dialog");
    const submit=dialog.getByRole("button",{name:"Start review again",exact:true});
    await expect(submit).toBeDisabled();
    await expect(dialog).toHaveClass(/sm:max-w-2xl/);
    await dialog.getByRole("textbox",{name:"Recovery reason"}).fill("The test connection is restored; review this delivery again.");
    await expect(submit).toBeDisabled();
    await dialog.getByRole("checkbox").check();await expect(submit).toBeEnabled();
    await page.screenshot({path:testInfo.outputPath("review-recovery-desktop.png"),fullPage:true});
    await page.setViewportSize({width:390,height:844});
    expect(await dialog.evaluate(node=>node.scrollWidth<=node.clientWidth)).toBe(true);
    await submit.scrollIntoViewIfNeeded();await expect(submit).toBeInViewport();
    await page.screenshot({path:testInfo.outputPath("review-recovery-mobile.png"),fullPage:true});
    const response=page.waitForResponse(r=>r.url().endsWith("/recover")&&r.request().method()==="POST");
    await submit.click();
    const recovered=await response;expect(recovered.status(),await recovered.text()).toBe(200);
    const result=await recovered.json();expect(result.task_id).not.toBe(result.failed_task_id);expect(result.allowed_attempts).toBe(3);expect(result.max_attempts).toBe(6);
    await expect(dialog).not.toBeVisible();
    await expect(page.getByText("Review is queued.",{exact:true})).toBeVisible();
    await expect(page.getByRole("button",{name:"Recover review",exact:true})).not.toBeVisible();
    const after=await api.getWorkflowRun(runId);expect(after.status).toBe("active");
  } finally {
    try {if(runId)await fixture("cleanup");} finally {await api.cleanup();}
  }
});
