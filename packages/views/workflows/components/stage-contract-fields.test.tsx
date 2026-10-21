import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import enWorkflows from "../../locales/en/workflows.json";
import { StageContractFields, type StageContractDraft } from "./stage-contract-fields";
import { validStageContractDraft } from "./stage-contract-validation";

const starter = { schema_version: "silieco.stage/v1", data: { type: "object" } };
function setup(value: StageContractDraft = {}) {
  const onChange = vi.fn();
  render(<I18nProvider locale="en" resources={{ en: { workflows: enWorkflows } }}><StageContractFields value={value} precedingStages={[{stableKey: "build", name: "Build"}]} onChange={onChange} /></I18nProvider>);
  fireEvent.click(screen.getByRole("button", { name: "Configure inputs and outputs" }));
  return onChange;
}
it("applies structured output fields without dropping descriptive configuration", () => {
  const onChange = setup({ inputSpec: { default_artifact: "Requirements" }, outputSpec: { default_artifact: "Report", custom: "keep" } });
  fireEvent.change(screen.getByLabelText("New field name"), { target: { value: "summary" } });
  fireEvent.click(screen.getByRole("button", {name: "Add field"}));
  fireEvent.click(screen.getByRole("checkbox", {name: "Required"}));
  fireEvent.click(screen.getByRole("button", {name: "Apply configuration"}));
  expect(onChange).toHaveBeenCalledExactlyOnceWith({ inputSpec: {default_artifact: "Requirements"}, inputBindings: {}, outputSpec: {default_artifact: "Report", custom: "keep", contract: {schema_version: "silieco.stage/v1", data: {type: "object", properties: {summary: {type: "string"}}, required: ["summary"]}}} });
});
it("preserves nested contracts, artifact constraints and explicit accepted-result bindings", () => {
  const nested = {schema_version: "silieco.stage/v1", data: {type: "object", properties: {rows: {type: "array", min_items: 1, items: {type: "object", properties: {score: {type: "number", minimum: 0, maximum: 1}}, required: ["score"]}}}}, artifacts: [{name: "report", kind: "file", media_type: "text/plain", schema_version: "v1", required: true}]};
  const value = {inputSpec: {contract: {schema_version: "silieco.stage/v1", data: {type: "object", properties: {score: {type: "number"}}}}}, inputBindings: {schema_version: "silieco.input-bindings/v1", data: [{target: "score", source: {kind: "stage_result", stage_key: "build", pointer: "/score"}}]}, outputSpec: {contract: nested}};
  const onChange = setup(value);
  fireEvent.click(screen.getByRole("button", {name: "Apply configuration"}));
  expect(onChange).toHaveBeenCalledExactlyOnceWith(value);
});
it("keeps invalid JSON local and discards it on cancel", () => {
  const onChange = setup({outputSpec: {contract: starter}});
  const section = screen.getByRole("heading", {name: "Output contract"}).closest("section")!;
  fireEvent.change(within(section).getByLabelText("Configuration JSON"), {target: {value: "{"}});
  expect(screen.getByRole("button", {name: "Apply configuration"})).toBeDisabled();
  fireEvent.click(screen.getByRole("button", {name: "Cancel"}));
  expect(onChange).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", {name: "Configure inputs and outputs"}));
  expect(screen.getByRole("button", {name: "Apply configuration"})).toBeEnabled();
});
it("requires complete delivery metadata before applying", () => {
  const onChange = setup();
  fireEvent.click(screen.getByRole("button", {name: "Add delivery"}));
  expect(screen.getByRole("button", {name: "Apply configuration"})).toBeDisabled();
  fireEvent.change(screen.getByLabelText("File name"), {target: {value: "report"}});
  fireEvent.click(screen.getByRole("button", {name: "Apply configuration"}));
  expect(onChange.mock.calls[0]?.[0].outputSpec.contract.artifacts).toEqual([{name: "report", kind: "file", media_type: "text/plain", schema_version: "v1", required: true}]);
});
it("rejects missing or future binding sources instead of silently changing them", () => {
  setup({inputSpec: {contract: {schema_version: "silieco.stage/v1", data: {type: "object", properties: {goal: {type: "string"}}}}}, inputBindings: {schema_version: "silieco.input-bindings/v1", data: [{target: "goal", source: {kind: "stage_result", stage_key: "missing", pointer: "/goal"}}]}});
  expect(screen.getByRole("button", {name: "Apply configuration"})).toBeDisabled();
  expect(screen.getByRole("alert")).toBeInTheDocument();
});
it.each([
  {type: "array"},
  {type: "integer", minimum: 5, maximum: 2},
  {type: "string", enum: [12]},
  {type: "object", properties: {}, required: ["missing"]},
  {type: "string", pattern: "unsupported"},
])("rejects an invalid nested field %j", (field) => {
  expect(validStageContractDraft({schema_version: "silieco.stage/v1", data: {type: "object", properties: {field}}}, true)).toBe(false);
});
it("rejects input file requirements without a supported source resolver", () => {
  expect(validStageContractDraft({...starter, artifacts: [{name:"file",kind:"file",media_type:"text/plain",schema_version:"v1"}]}, false)).toBe(false);
});
