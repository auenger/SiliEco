import {fireEvent,render,screen} from "@testing-library/react";
import {expect,it,vi} from "vitest";
import {I18nProvider} from "@silieco/core/i18n/react";
import enWorkflows from "../../locales/en/workflows.json";
import {StageRetryFields,validRetryPolicyDraft} from "./stage-retry-fields";
function setup(value={},disabled=false) {const onChange=vi.fn();render(<I18nProvider locale="en" resources={{en:{workflows:enWorkflows}}}><StageRetryFields value={value} disabled={disabled} onChange={onChange}/></I18nProvider>);return onChange;}
it("sets a retry budget while retaining the timeout",()=>{const onChange=setup({timeout_seconds:60});fireEvent.change(screen.getByRole("spinbutton",{name:"Maximum attempts"}),{target:{value:"1"}});expect(onChange).toHaveBeenLastCalledWith({max_attempts:1,timeout_seconds:60});});
it("clears dependent backoff values when restoring defaults",()=>{const onChange=setup({max_attempts:2,backoff_seconds:5,backoff_multiplier:2,max_backoff_seconds:60});fireEvent.change(screen.getByRole("spinbutton",{name:"Initial retry delay (seconds)"}),{target:{value:""}});expect(onChange).toHaveBeenLastCalledWith({max_attempts:2});});
it("keeps frozen configuration read-only",()=>{setup({},true);for(const field of screen.getAllByRole("spinbutton"))expect(field).toBeDisabled();});
it("shows dependent range errors",()=>{setup({backoff_seconds:60,max_backoff_seconds:30});expect(screen.getByRole("alert")).toHaveTextContent("maximum must be at least");});
it.each([{max_attempts:0},{max_attempts:11},{timeout_seconds:0},{timeout_seconds:86401},{backoff_seconds:1.5},{backoff_multiplier:2},{backoff_seconds:10,max_backoff_seconds:5}])("rejects invalid retry configuration %j",value=>expect(validRetryPolicyDraft(value)).toBe(false));
it("accepts omitted defaults and immediate retries",()=>{expect(validRetryPolicyDraft()).toBe(true);expect(validRetryPolicyDraft({max_attempts:1,backoff_seconds:0})).toBe(true);});
