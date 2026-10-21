import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { Agent } from "@silieco/core/types";
import enWorkflows from "../../locales/en/workflows.json";
import { StageExecutionFields } from "./stage-execution-fields";

function setup(value:Parameters<typeof StageExecutionFields>[0]["value"],disabled=false) {
 const onChange=vi.fn();render(<I18nProvider locale="en" resources={{en:{workflows:enWorkflows}}}><StageExecutionFields value={value} agents={[{id:"a",name:"Builder",archived_at:null}] as Agent[]} disabled={disabled} onChange={onChange}/></I18nProvider>);return onChange;
}
it("enables agent execution with a server-valid completion rule and starter contract",()=>{
 const onChange=setup({executionMode:"manual",outputSpec:{default_artifact:"Report"}});
 fireEvent.click(screen.getByRole("radio",{name:/^Agent/}));
 expect(onChange).toHaveBeenCalledExactlyOnceWith({executionMode:"agent",completionRule:{type:"validated_result"},outputSpec:{default_artifact:"Report",contract:{schema_version:"silieco.stage/v1",data:{type:"object"}}}});
});
it("preserves an existing output contract while changing execution mode",()=>{
 const outputSpec={contract:{schema_version:"silieco.stage/v1",data:{type:"object",required:["summary"]}}};
 const onChange=setup({executionMode:"manual",outputSpec});fireEvent.click(screen.getByRole("radio",{name:/^Agent/}));expect(onChange.mock.calls[0]?.[0].outputSpec).toEqual(outputSpec);
});
it("shows an unresolved executor instead of silently choosing the reviewer",()=>{
 setup({executionMode:"agent",executorId:"missing"});expect(screen.getByRole("combobox",{name:"Execution agent"})).toBeInTheDocument();expect(screen.getByText("Choose an available agent before saving.")).toBeInTheDocument();
});
it("keeps frozen execution fields read-only",()=>{
 const onChange=setup({executionMode:"agent",executorId:"a"},true);expect(screen.getByRole("radio",{name:/^Manual/})).toBeDisabled();expect(screen.getByRole("combobox",{name:"Execution agent"})).toBeDisabled();fireEvent.click(screen.getByRole("radio",{name:/^Manual/}));expect(onChange).not.toHaveBeenCalled();
});
it("clears the executor when switching back to manual execution",()=>{
 const onChange=setup({executionMode:"agent",executorId:"a"});fireEvent.click(screen.getByRole("radio",{name:/^Manual/}));expect(onChange).toHaveBeenCalledExactlyOnceWith({executionMode:"manual",executorId:null,completionRule:{type:"all_tasks_terminal"}});
});
it("edits an explicit rework limit and can remove the configured limit",()=>{
 const onChange=setup({executionMode:"manual",reworkPolicy:{max_reworks:3}});
 fireEvent.change(screen.getByRole("spinbutton",{name:"Maximum reworks"}),{target:{value:"0"}});
 expect(onChange).toHaveBeenLastCalledWith({reworkPolicy:{max_reworks:0}});
 fireEvent.change(screen.getByRole("spinbutton",{name:"Maximum reworks"}),{target:{value:""}});
 expect(onChange).toHaveBeenLastCalledWith({reworkPolicy:{}});
});
it("reports invalid fractional rework limits",()=>{setup({executionMode:"manual",reworkPolicy:{max_reworks:1.5}});expect(screen.getByRole("alert")).toHaveTextContent("whole number");});
it("removes executor options after selecting an agent",async()=>{
 const onChange=setup({executionMode:"agent"});const trigger=screen.getByRole("combobox",{name:"Execution agent"});fireEvent.click(trigger);
 const option=await screen.findByRole("option",{name:"Builder"});fireEvent.mouseMove(option);fireEvent.click(option);
 expect(onChange).toHaveBeenLastCalledWith({executorId:"a"});expect(trigger).toHaveAttribute("aria-expanded","false");expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});
