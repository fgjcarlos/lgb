import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Backup } from "@/pages/Backup";

vi.mock("@/contexts/auth", () => ({
  useAuth: () => ({ getWsToken: () => "tok" }),
}));

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) =>
      path === "/api/backup/status"
        ? json({ status: "idle", restore: { status: "running" } })
        : json({ data: [] }),
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Backup", () => {
  it("registers the restore polling interval once across re-renders", async () => {
    const setIntervalSpy = vi.spyOn(window, "setInterval");
    const client = new QueryClient();
    const tree = () => (
      <QueryClientProvider client={client}>
        <Backup />
      </QueryClientProvider>
    );
    const { rerender } = render(tree());
    expect(await screen.findByText("Restoring")).toBeInTheDocument();

    rerender(tree());
    rerender(tree());

    const restorePolls = setIntervalSpy.mock.calls.filter(([, ms]) => ms === 2000);
    expect(restorePolls).toHaveLength(1);
  });
});
