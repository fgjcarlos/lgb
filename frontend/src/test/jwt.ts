import type { JwtPayload } from "@/lib/jwt";

/** Builds an unsigned JWT whose payload segment is base64url-encoded JSON. */
export function makeToken(payload: unknown): string {
  const b64url = (s: string) =>
    btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `${b64url('{"alg":"HS256","typ":"JWT"}')}.${b64url(JSON.stringify(payload))}.sig`;
}

export function claims(overrides: Partial<JwtPayload> = {}): JwtPayload {
  const now = Math.floor(Date.now() / 1000);
  return {
    uid: 7,
    sub: "alice",
    role: "operator",
    exp: now + 3600,
    iat: now,
    iss: "lgb",
    ...overrides,
  };
}
