import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider, useAuth } from "@/contexts/auth";
import { claims, makeToken } from "@/test/jwt";

type Handler = (init: RequestInit | undefined) => Response;

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

let routes: Record<string, Handler>;
const fetchMock = vi.fn(async (path: string, init?: RequestInit) => {
  const handler = routes[`${init?.method ?? "GET"} ${path}`];
  if (!handler) throw new Error(`unexpected request ${init?.method ?? "GET"} ${path}`);
  return handler(init);
});

const calls = () => fetchMock.mock.calls.map(([path, init]) => `${init?.method ?? "GET"} ${path}`);

let api: ReturnType<typeof useAuth>;
function Probe() {
  api = useAuth();
  return (
    <div>
      <span data-testid="user">{api.user ? `${api.user.username}:${api.user.role}` : "anon"}</span>
      <span data-testid="token">{api.getWsToken() ?? "none"}</span>
    </div>
  );
}

const renderAuth = () =>
  render(
    <AuthProvider>
      <Probe />
    </AuthProvider>,
  );

const token = makeToken(claims());
// Generated per run so the fixture never looks like a stored credential.
const password = crypto.randomUUID();

beforeEach(() => {
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
  routes = {
    "GET /api/auth/me": () => json(401, { error: { code: "unauthorized", message: "no session" } }),
    "POST /api/auth/logout": () => new Response(null, { status: 204 }),
    "POST /api/auth/login": () => json(200, { token, expires_at: "2099-01-01T00:00:00Z" }),
  };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("AuthProvider", () => {
  it("starts anonymous when there is no session", async () => {
    renderAuth();
    await waitFor(() => expect(calls()).toContain("GET /api/auth/me"));
    expect(screen.getByTestId("user")).toHaveTextContent("anon");
    expect(screen.getByTestId("token")).toHaveTextContent("none");
  });

  it("restores a session cookie and fetches a WS token", async () => {
    routes["GET /api/auth/me"] = () =>
      json(200, {
        user: { id: 7, username: "alice", role: "operator" },
        expires_at: "2099-01-01T00:00:00Z",
      });
    routes["POST /api/auth/refresh"] = () =>
      json(200, { token, expires_at: "2099-01-01T00:00:00Z" });

    renderAuth();
    await waitFor(() => expect(screen.getByTestId("token")).toHaveTextContent(token));
    expect(screen.getByTestId("user")).toHaveTextContent("alice:operator");
  });

  it("logs in with credentials and logs out", async () => {
    renderAuth();
    await waitFor(() => expect(calls()).toContain("GET /api/auth/me"));

    await act(() => api.login("alice", password));
    expect(screen.getByTestId("user")).toHaveTextContent("alice:operator");
    expect(screen.getByTestId("token")).toHaveTextContent(token);
    const loginCall = fetchMock.mock.calls.find(([p]) => p === "/api/auth/login");
    expect(JSON.parse(String(loginCall?.[1]?.body))).toEqual({
      username: "alice",
      password,
    });

    fetchMock.mockClear();
    await act(() => api.logout());
    expect(calls()).toEqual(["POST /api/auth/logout"]);
    expect(screen.getByTestId("user")).toHaveTextContent("anon");
    expect(screen.getByTestId("token")).toHaveTextContent("none");
  });

  it("clears local state on logout even when the server call fails", async () => {
    renderAuth();
    await waitFor(() => expect(calls()).toContain("GET /api/auth/me"));
    await act(() => api.login("alice", password));

    routes["POST /api/auth/logout"] = () => json(500, {});
    await act(() => api.logout());
    expect(screen.getByTestId("user")).toHaveTextContent("anon");
  });

  it("rejects a login whose token claims are invalid", async () => {
    routes["POST /api/auth/login"] = () =>
      json(200, { token: makeToken({ role: "admin", uid: 1 }), expires_at: "x" });
    renderAuth();
    await waitFor(() => expect(calls()).toContain("GET /api/auth/me"));

    await expect(act(() => api.login("mallory", password))).rejects.toThrow(/invalid JWT claims/);
    expect(screen.getByTestId("user")).toHaveTextContent("anon");
  });

  it("rejects a failed login with the server message", async () => {
    routes["POST /api/auth/login"] = () =>
      json(429, {
        error: { code: "too_many_requests", message: "too many login attempts, try again later" },
      });
    renderAuth();
    await waitFor(() => expect(calls()).toContain("GET /api/auth/me"));

    await expect(act(() => api.login("alice", password))).rejects.toThrow(
      "too many login attempts, try again later",
    );
    expect(screen.getByTestId("user")).toHaveTextContent("anon");
  });
});
