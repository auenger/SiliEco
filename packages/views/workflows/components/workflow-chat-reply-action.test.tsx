import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import { attachWorkflowEditor, workflowDesignerPrompt, useWorkflowChatEditorStore } from "@silieco/core/workflows";
import { WorkflowChatReplyAction } from "./workflow-chat-reply-action";
import en from "../../locales/en/workflows.json";
const chat = vi.hoisted(() => ({ setExpanded: vi.fn() }));
vi.mock("@silieco/core/chat", () => ({ useChatStore: { getState: () => chat } }));
const draft = {name:"SOP",description:"",stages:[{id:"one",name:"One",description:"",input:"",output:"Before"}]};
const apply = vi.fn();
beforeEach(() => { apply.mockClear(); attachWorkflowEditor({id:"editor",wsId:"ws",portalId:"target",read:()=>draft,selection:()=>"one",apply,snapshot:()=>vi.fn()}); });
function view(content: string) { return <I18nProvider locale="en" resources={{en:{workflows:en}}}><WorkflowChatReplyAction wsId="ws" sessionId="session" taskId="task" pending={false} prompt={workflowDesignerPrompt("Edit",draft)} content={content}/></I18nProvider>; }
it("fills a saved fenced reply into the bound editor", () => {
  const updated={...draft,stages:[{...draft.stages[0]!,output:"After"}]};
  render(view('<sop_draft>\n```json\n'+JSON.stringify(updated)+'\n```\n</sop_draft>'));
  fireEvent.click(screen.getByRole('button',{name:'Fill current SOP'}));
  expect(apply).toHaveBeenCalledExactlyOnceWith(updated);
  expect(useWorkflowChatEditorStore.getState().status).toBe('applied');
});
it("shows invalid reply feedback and disables filling", () => {
  render(view('<sop_draft>{}</sop_draft>'));
  expect(screen.getByRole('button',{name:'Fill current SOP'})).toBeDisabled();
  expect(screen.getByText('The reply could not be applied. Ask the agent to try again.')).toBeVisible();
});

it("shows conflicts beside the clicked button and requires explicit resolution", () => {
  const current = {...draft, stages:[{...draft.stages[0]!,output:"Manual version"}]};
  attachWorkflowEditor({id:"editor",wsId:"ws",portalId:"target",read:()=>current,selection:()=>"one",apply,snapshot:()=>vi.fn()});
  const updated={...draft,stages:[{...draft.stages[0]!,output:"Agent version"}]};
  render(view('<sop_draft>'+JSON.stringify(updated)+'</sop_draft>'));
  fireEvent.click(screen.getByRole('button',{name:'Fill current SOP'}));
  expect(apply).not.toHaveBeenCalled();
  expect(screen.getByText('Manual version')).toBeVisible();
  expect(screen.getByText('Agent version')).toBeVisible();
  fireEvent.click(screen.getByRole('button',{name:'Use reply for conflicting fields'}));
  expect(apply).toHaveBeenCalledExactlyOnceWith(updated);
  expect(screen.getByRole('status')).toHaveTextContent('SOP draft updated');
  expect(chat.setExpanded).toHaveBeenCalledWith(false);
});
it("explains when the current content already matches the reply", () => {
  render(view('<sop_draft>'+JSON.stringify(draft)+'</sop_draft>'));
  fireEvent.click(screen.getByRole('button',{name:'Fill current SOP'}));
  expect(screen.getByRole('status')).toHaveTextContent('already matches');
  expect(apply).not.toHaveBeenCalled();
});
