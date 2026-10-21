---
name: silieco-thermal-requirements
description: "Use in the thermal simulation POC requirement-extraction Stage to turn uploaded files and user context into a sourced, reviewable thermal requirements draft."
user-invocable: false
---

# Extract Thermal Requirements

## Input

- The current Task, comments, Project context, and uploaded specifications, spreadsheets, images, or model files.
- Any explicit operating condition, component limit, power, airflow, pressure-drop, acoustic, cost, or geometry constraint.

## Procedure

1. Inspect the available attachment metadata before downloading only the files needed for extraction. Use `silieco attachment download <id>` when the Task context provides an attachment ID.
2. Extract facts with their source location. Preserve the original unit and add a normalized SI value only when the conversion is unambiguous.
3. Separate confirmed facts, engineering assumptions, and missing information. Never turn a Demo default into a customer requirement.
4. Require explicit confirmation later for `target_tmax_c`, ambient/operating condition, critical components, and the model file to simulate.

## Required output

Produce `requirements-draft.json` with `schema_version`, `case_name`, `model_ref`, `operating_conditions`, `target_tmax_c`, `critical_components`, `constraints`, `sources`, `assumptions`, `missing_fields`, and `confirmed:false`. Also produce `missing-information.md` containing only questions that can change model validity, acceptance, safety, or the simulated result.

Post a concise summary to the current Task. Do not approve the human Gate, invent missing limits, start a simulation, or advance the SOP.
