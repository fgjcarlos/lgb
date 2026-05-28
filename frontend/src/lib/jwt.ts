export interface JwtPayload {
  uid: number;
  sub: string;
  role: "admin" | "operator" | "viewer";
  exp: number;
  iat: number;
  iss: string;
}

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
  return JSON.parse(json) as JwtPayload;
}

export function userFromPayload(p: JwtPayload): AuthUser {
  return { id: p.uid, username: p.sub, role: p.role };
}

export function isTokenExpired(p: JwtPayload, nowSeconds = Date.now() / 1000): boolean {
  return p.exp <= nowSeconds;
}
