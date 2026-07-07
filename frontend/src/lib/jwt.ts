import { z } from "zod";

// jwtPayloadSchema is the runtime validator for the claims the server
// issues. Anything that does not match this shape must NOT drive
// ProtectedRoute — the user would get routed against attacker-controlled
// role / id values (Fix for #78 — was a typed cast with no validation
// before; a malicious script could have populated localStorage with a
// payload of shape {role:"admin", uid:1} and bypassed ACL entirely).
export const jwtPayloadSchema = z.object({
  uid: z.number().int().positive(),
  sub: z.string().min(1),
  role: z.enum(["admin", "operator", "viewer"]),
  exp: z.number().int().positive(),
  iat: z.number().int().positive(),
  iss: z.string().min(1),
});

export type JwtPayload = z.infer<typeof jwtPayloadSchema>;

export interface AuthUser {
  id: number;
  username: string;
  role: JwtPayload["role"];
}

export function decodeJwtPayload(token: string): JwtPayload {
  const parts = token.split(".");
  if (parts.length < 2) {
    throw new Error("malformed JWT: expected 3 dot-separated segments");
  }
  const base64 = parts[1].replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64 + "===".slice((base64.length + 3) % 4);
  const json = atob(padded);
  // Fix for #78: validate the decoded claims against the Zod schema
  // before trusting them for routing. The cast is gone — if the payload
  // does not match, the caller falls back to the unauthenticated state
  // instead of reading attacker-controlled values.
  const parsed = jwtPayloadSchema.safeParse(JSON.parse(json));
  if (!parsed.success) {
    throw new Error(`invalid JWT claims: ${parsed.error.issues[0]?.message ?? "unknown"}`);
  }
  return parsed.data;
}

export function userFromPayload(p: JwtPayload): AuthUser {
  return { id: p.uid, username: p.sub, role: p.role };
}

export function isTokenExpired(p: JwtPayload, nowSeconds = Date.now() / 1000): boolean {
  return p.exp <= nowSeconds;
}

// sessionUserSchema mirrors the server's /api/auth/me response shape.
// Validating it guards against a server-side regression silently
// breaking the auth flow.
export const sessionUserSchema = z.object({
  user: z.object({
    id: z.number().int().positive(),
    username: z.string().min(1),
    role: z.enum(["admin", "operator", "viewer"]),
  }),
  expires_at: z.string().refine((s) => !Number.isNaN(Date.parse(s)), {
    message: "expires_at must be a valid ISO timestamp",
  }),
});

export type SessionUser = z.infer<typeof sessionUserSchema>;
export type AuthRole = JwtPayload["role"];