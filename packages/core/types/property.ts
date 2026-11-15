/**
 * Custom task properties — workspace-defined, typed fields on tasks
 * (SILI-4463). Definitions live in a workspace catalog (managed by owner/admin
 * only); values live on each task in a bag keyed by definition id, so
 * renames never touch task rows.
 *
 * Values are typed per definition: select stores an option id, multi_select
 * an array of option ids (config order), date a "YYYY-MM-DD" string, checkbox
 * a boolean, number a number, text/url strings.
 */
export type TaskPropertyType =
  | "text"
  | "number"
  | "select"
  | "multi_select"
  | "date"
  | "checkbox"
  | "url";

export const TASK_PROPERTY_TYPES: TaskPropertyType[] = [
  "text",
  "number",
  "select",
  "multi_select",
  "date",
  "checkbox",
  "url",
];

export function isKnownPropertyType(type: string): type is TaskPropertyType {
  return (TASK_PROPERTY_TYPES as string[]).includes(type);
}

export interface TaskPropertyOption {
  id: string;
  name: string;
  /** Normalized lowercase hex color, e.g. `#3b82f6`. */
  color: string;
}

export interface TaskPropertyConfig {
  options?: TaskPropertyOption[];
}

export interface TaskProperty {
  id: string;
  workspace_id: string;
  name: string;
  /** Lenient string: newer servers may ship types this client doesn't know. */
  type: string;
  description?: string;
  /** Optional catalog icon key; absent on backends predating icon support. */
  icon?: string;
  config: TaskPropertyConfig;
  position: number;
  archived: boolean;
  archived_at?: string | null;
  usage_count?: number;
  created_at: string;
  updated_at: string;
}

export type TaskPropertyValue = string | number | boolean | string[];
export type TaskPropertyValues = Record<string, TaskPropertyValue>;

export interface CreatePropertyRequest {
  name: string;
  type: TaskPropertyType;
  description?: string;
  icon?: string;
  config?: TaskPropertyConfig;
}

export interface UpdatePropertyRequest {
  name?: string;
  description?: string;
  /** Empty string clears the icon. */
  icon?: string;
  config?: TaskPropertyConfig;
  archived?: boolean;
}

export interface ListPropertiesResponse {
  properties: TaskProperty[];
  total: number;
}

/** Response of PUT/DELETE /api/tasks/{id}/properties/{propertyId}: the full post-mutation bag. */
export interface TaskPropertiesResponse {
  properties: TaskPropertyValues;
}
