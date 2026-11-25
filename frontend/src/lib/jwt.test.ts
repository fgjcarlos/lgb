import { describe, expect, it } from "vitest";
import { decodeJwtPayload, isTokenExpired, userFromPayload } from "@/lib/jwt";
import { claims, makeToken } from "@/test/jwt";

describe("decodeJwtPayload", () => {
  it("decodes valid base64url claims", () => {
    const c = claims({ sub: "ünïcode?>>" }); // forces '-' / '_' in base64url
    expect(decodeJwtPayload(makeToken(c))).toEqual(c);
  });

  it("rejects a token without a payload segment", () => {
    expect(() => decodeJwtPayload("onlyone")).toThrow(
      "malformed JWT: expected 3 dot-separated segments",
    );
  });

  it("rejects claims that fail the schema", () => {
    expect(() => decodeJwtPayload(makeToken({ ...claims(), role: "root" }))).toThrow(
      /^invalid JWT claims:/,
    );
    expect(() => decodeJwtPayload(makeToken({ role: "admin", uid: 1 }))).toThrow(
      /^invalid JWT claims:/,
    );
  });

  it("rejects a payload that is not JSON", () => {
    expect(() => decodeJwtPayload("a.bm90LWpzb24.c")).toThrow();
  });
});

describe("isTokenExpired", () => {
  it("is expired at and after exp, not before", () => {
    const c = claims({ exp: 1000 });
    expect(isTokenExpired(c, 999)).toBe(false);
    expect(isTokenExpired(c, 1000)).toBe(true);
    expect(isTokenExpired(c, 1001)).toBe(true);
  });

  it("defaults to the current time", () => {
    const now = Math.floor(Date.now() / 1000);
    expect(isTokenExpired(claims({ exp: now + 60 }))).toBe(false);
    expect(isTokenExpired(claims({ exp: now - 60 }))).toBe(true);
  });
});

describe("userFromPayload", () => {
  it("maps uid/sub/role", () => {
    expect(userFromPayload(claims())).toEqual({ id: 7, username: "alice", role: "operator" });
  });
});
