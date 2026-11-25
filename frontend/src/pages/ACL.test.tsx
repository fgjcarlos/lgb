import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ACL } from "@/pages/ACL";

vi.mock("@/contexts/auth", () => ({
  useAuth: () => ({ getWsToken: () => "tok" }),
}));

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn(async (path: string, init?: RequestInit) => {
    if (path === "/api/plcs") {
      return json({ data: [{ name: "p1", tags: [{ name: "t1", writable: true, type: "Int16" }] }] });
    }
    if (path === "/api/acl/rules" && init?.method === "POST") {
      return json({ error: { code: "internal", message: "store unavailable" } }, 500);
    }
    return json({ data: [] });
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ACL", () => {
  it("shows an inline error and keeps the server state when a change fails", async () => {
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
        <ACL />
      </QueryClientProvider>,
    );
    const box = await screen.findByRole("checkbox", { name: "Allow operator to write p1/t1" });
    const ruleFetches = () =>
      fetchMock.mock.calls.filter(([p, i]) => p === "/api/acl/rules" && !i?.method).length;
    const before = ruleFetches();

    await userEvent.click(box);

    expect(await screen.findByRole("alert")).toHaveTextContent("store unavailable");
    expect(box).not.toBeChecked();
    // The matrix re-reads the server so the cell shows what actually applies.
    await waitFor(() => expect(ruleFetches()).toBe(before + 1));
  });
});
