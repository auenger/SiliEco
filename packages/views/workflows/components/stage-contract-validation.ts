import type { WorkflowObject } from "@silieco/core/types";

function object(value: unknown): WorkflowObject | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as WorkflowObject : null;
}
function keys(value: WorkflowObject, allowed: readonly string[]) {
  return Object.keys(value).every((key) => allowed.includes(key));
}

// Draft preflight keeps incomplete form values local. The server remains the
// authority for contract semantics and access to accepted input revisions.
export function validStageContractDraft(value: unknown, allowArtifacts: boolean): value is WorkflowObject {
  const contract = object(value);
  if (!contract || contract.schema_version !== "silieco.stage/v1" || !keys(contract, ["schema_version", "data", "artifacts"])) return false;
  let nodes = 0;
  function schema(value: unknown, depth: number): boolean {
    const node = object(value);
    if (!node || ++nodes > 1024 || depth > 32 || !keys(node, ["type", "properties", "required", "items", "enum", "minimum", "maximum", "min_items", "max_items"])) return false;
    if (typeof node.type !== "string" || !["object", "array", "string", "boolean", "number", "integer"].includes(node.type)) return false;
    if (node.type !== "object" && (node.properties !== undefined || node.required !== undefined)) return false;
    if (node.type !== "array" && (node.items !== undefined || node.min_items !== undefined || node.max_items !== undefined)) return false;
    if (node.type !== "number" && node.type !== "integer" && (node.minimum !== undefined || node.maximum !== undefined)) return false;
    for (const key of ["minimum", "maximum", "min_items", "max_items"]) {
      const limit = node[key];
      if (limit !== undefined && (typeof limit !== "number" || !Number.isFinite(limit))) return false;
      if ((key === "min_items" || key === "max_items") && limit !== undefined && (!Number.isInteger(limit) || (limit as number) < 0)) return false;
    }
    if (typeof node.minimum === "number" && typeof node.maximum === "number" && node.minimum > node.maximum) return false;
    if (typeof node.min_items === "number" && typeof node.max_items === "number" && node.min_items > node.max_items) return false;
    if (node.type === "object") {
      const properties = node.properties === undefined ? {} : object(node.properties);
      if (!properties || !Object.values(properties).every((child) => schema(child, depth + 1))) return false;
      if (node.required !== undefined && (!Array.isArray(node.required) || new Set(node.required).size !== node.required.length || !node.required.every((key: unknown) => typeof key === "string" && Object.hasOwn(properties, key)))) return false;
    }
    if (node.type === "array" && !schema(node.items, depth + 1)) return false;
    if (node.enum !== undefined) {
      if (!Array.isArray(node.enum) || node.enum.length === 0 || node.type === "object" || node.type === "array") return false;
      if (!node.enum.every((item: unknown) => {
        if (node.type === "integer" ? typeof item !== "number" || !Number.isInteger(item) : typeof item !== node.type) return false;
        return typeof item !== "number" || Number.isFinite(item) && (typeof node.minimum !== "number" || item >= node.minimum) && (typeof node.maximum !== "number" || item <= node.maximum);
      })) return false;
    }
    return true;
  }
  if (object(contract.data)?.type !== "object" || !schema(contract.data, 0)) return false;
  if (contract.artifacts === undefined) return true;
  if (!Array.isArray(contract.artifacts) || !allowArtifacts && contract.artifacts.length > 0) return false;
  const names = new Set<string>();
  return contract.artifacts.every((raw: unknown) => {
    const artifact = object(raw);
    if (!artifact || !keys(artifact, ["name", "kind", "media_type", "schema_version", "required"])) return false;
    if (!["name", "kind", "media_type", "schema_version"].every((key) => typeof artifact[key] === "string" && (artifact[key] as string).trim().length > 0)) return false;
    if (artifact.required !== undefined && typeof artifact.required !== "boolean" || names.has(artifact.name as string)) return false;
    names.add(artifact.name as string);
    return true;
  });
}
