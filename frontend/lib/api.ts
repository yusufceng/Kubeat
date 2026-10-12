const BACKEND_URL = process.env.BACKEND_URL ?? "http://localhost:8080";

export type Health = {
  status: string;
  version: string;
  time: string;
};

export async function getHealth(): Promise<Health> {
  const res = await fetch(`${BACKEND_URL}/api/v1/health`, {
    signal: AbortSignal.timeout(3000),
  });
  if (!res.ok) {
    throw new Error(`backend responded with ${res.status}`);
  }
  return res.json();
}
