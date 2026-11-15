/**
 * Thin wrapper around expo-secure-store for the auth token.
 * The protocol generation isolates credentials from discarded test environments.
 * Old credentials are never restored into the Task / AgentRun contract.
 */
import * as SecureStore from "expo-secure-store";

import { PROTOCOL_GENERATION } from "./protocol";

const TOKEN_KEY = `silieco_${PROTOCOL_GENERATION}_token`;

export async function getToken(): Promise<string | null> {
  return SecureStore.getItemAsync(TOKEN_KEY);
}

export async function setToken(token: string): Promise<void> {
  await SecureStore.setItemAsync(TOKEN_KEY, token);
}

export async function clearToken(): Promise<void> {
  await SecureStore.deleteItemAsync(TOKEN_KEY);
}
