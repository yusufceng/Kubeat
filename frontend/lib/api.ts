const BACKEND_URL = process.env.BACKEND_URL ?? "http://localhost:8080";
const BACKEND_TIMEOUT_MS = Number(process.env.BACKEND_TIMEOUT_MS) || 3000;

export type Health = {
  status: string;
  version: string;
  time: string;
};

export async function getHealth(): Promise<Health> {
  const res = await fetch(`${BACKEND_URL}/api/v1/health`, {
    signal: AbortSignal.timeout(BACKEND_TIMEOUT_MS),
  });
  if (!res.ok) {
    throw new Error(`backend responded with ${res.status}`);
  }
  return res.json();
}
