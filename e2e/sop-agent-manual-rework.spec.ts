import {test,expect} from "@playwright/test";
import {execFile} from "node:child_process";
import {promisify} from "node:util";
import {createTestApi} from "./helpers";
const execute=promisify(execFile);
test("agent rework returns to manual input and starts a fresh agent attempt",async({page},testInfo)=>{
 test.setTimeout(300000);test.skip(!process.env.DATABASE_URL,"Controlled fixture requires acceptance database");
 const api=await createTestApi();let runId="";
 const fixture=(mode:string)=>execute("go",["test","./internal/service","-run","^TestWorkflowReviewBrowserFixture$","-count=1"],{cwd:"core",env:{...process.env,SILIECO_REVIEW_BROWSER_RUN:runId,SILIECO_REVIEW_BROWSER_MODE:mode},timeout:120000});
 try {
  const run=await api.createManualWorkflowRun(`Review recovery browser ${Date.now()}`);runId=run.runId;await fixture("rework");
  const token=api.getToken();if(!token)throw new Error("Missing login");
  await page.addInitScript(token=>{localStorage.setItem("silieco_token",token);localStorage.setItem("silieco:chat:isOpen","false");},token);
  await page.setViewportSize({width:1440,height:1000});
  await page.goto(`/${api.getWorkspaceSlug()}/projects/${run.projectId}?run=${runId}`,{waitUntil:"domcontentloaded"});
  await page.getByRole("button",{name:"Rework",exact:true}).click();
  const dialog=page.getByRole("dialog");await dialog.getByRole("radio",{name:/Manual input/}).check();
  const submit=dialog.getByRole("button",{name:"Start rework",exact:true});await expect(submit).toBeDisabled();
  await dialog.getByRole("textbox").fill("Correct the manual requirements before rebuilding");await dialog.getByRole("checkbox").check();
  await page.screenshot({path:testInfo.outputPath("agent-manual-rework-desktop.png"),fullPage:true});
  await page.setViewportSize({width:390,height:844});expect(await dialog.evaluate(node=>node.scrollWidth<=node.clientWidth)).toBe(true);
  await submit.scrollIntoViewIfNeeded();await expect(submit).toBeInViewport();await page.screenshot({path:testInfo.outputPath("agent-manual-rework-mobile.png"),fullPage:true});
  const response=page.waitForResponse(r=>r.url().endsWith("/rework")&&r.request().method()==="POST");await submit.click();
  const returned=await response;expect(returned.status(),await returned.text()).toBe(200);const result=await returned.json();expect(result.execution_id).toBeNull();expect(result.attempt).toBeNull();expect(result.status).toBe("awaiting_work");expect(result.rework_id).toBeTruthy();
  await expect(dialog).not.toBeVisible();await expect(page.getByRole("button",{name:"Review Stage",exact:true})).toBeVisible();
  const after=await api.getWorkflowRun(runId);expect(after.current_stage_id).toBe(result.stage_id);expect(after.tasks.find((task:{title:string})=>task.title==="Previous manual input").workflow_invalidated_by_rework_id).toBe(result.rework_id);
  await api.createIssue("Corrected manual input",{status:"done",project_id:run.projectId,workflow_instance_id:runId,workflow_stage_id:result.stage_id});
  await page.setViewportSize({width:1440,height:1000});await page.reload({waitUntil:"domcontentloaded"});
  await page.getByRole("button",{name:"Review Stage",exact:true}).click();
  const advanced=page.waitForResponse(r=>r.url().endsWith("/transition")&&r.request().method()==="POST");
  await page.getByRole("dialog").getByRole("button",{name:"Approve and enter next Stage"}).click();expect((await advanced).status()).toBe(200);
  await expect(page.getByText("Attempt 2 · Result version 0",{exact:true})).toBeVisible();
 }finally{try{if(runId)await fixture("cleanup");}finally{await api.cleanup();}}
});
