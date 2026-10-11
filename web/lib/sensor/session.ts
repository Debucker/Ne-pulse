"use client";

// One SensorClient per tab, kept outside React so streaming survives page
// navigation inside the app.
import { SensorClient, storage, type SensorState } from "./client";
import { attachNightLog, detachNightLog } from "./nightlog";

let client: SensorClient | null = null;

export function currentClient(): SensorClient | null {
  return client;
}

export function startStreaming(): SensorClient | null {
  if (client && client.state.phase !== "stopped" && client.state.phase !== "error") return client;
  const creds = storage.creds();
  const loc = storage.location();
  if (!creds || !loc) return null;
  client = new SensorClient(creds, loc);
  client.start();
  attachNightLog(client);
  return client;
}

export function stopStreaming() {
  detachNightLog();
  client?.stop();
}

export type { SensorState };
