---
name: silieco-thermal-model-precheck
description: "Use in the thermal simulation POC model-precheck Stage to run the deterministic Mock file validator and explain its deliberately limited evidence."
user-invocable: false
---

# Precheck Thermal Model

## Input

- The downloaded model file selected by the confirmed requirement snapshot.
- `requirements-snapshot.json`.
- The required `silieco-thermal-mock-runner` skill.

## Procedure

Run the Mock Runner `validate-model` command and write `model-validation.json`. A VALID result proves only that the file is non-empty, has an allowed POC extension, and has a stable hash. It does not prove that AEDT can open the project or that Design, Setup, boundaries, materials, mesh, monitors, or licenses are valid.

## Required output

Post `model-validation.json` and a short limitation note to the current Task. Treat `INVALID` as a blocker. Never rename an unrelated file to bypass validation, modify the source model, claim a real Icepak check, or advance the SOP.
