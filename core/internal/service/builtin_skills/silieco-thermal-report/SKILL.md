---
name: silieco-thermal-report
description: "Use in the thermal simulation POC delivery Stage to assemble a traceable Markdown report and artifact index without presenting Mock evidence as engineering validation."
user-invocable: false
---

# Deliver Thermal POC Report

## Input

- Confirmed requirements and acceptance profile.
- Model validation, baseline result/verdict, candidate decision/result/verdict, and their execution records.
- The required `silieco-thermal-mock-runner` skill.

## Procedure

Run the Mock Runner `generate-report` command, then verify that every referenced artifact exists. Clearly label the solver as `mock-thermal-poc-v1`, distinguish execution success from thermal verdict, and state that no AEDT, PyAEDT, license, mesh, or real thermal solve was used.

## Required output

Produce `thermal-poc-report.md` and `artifact-index.json`. Post a concise delivery summary to the current Task for the final human Gate. State that this is not a real engineering sign-off. Do not approve the Gate, present the report as real engineering evidence, or advance/archive the SOP.
