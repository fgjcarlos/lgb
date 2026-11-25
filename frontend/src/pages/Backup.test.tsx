import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

  it("asks for confirmation before triggering a backup", async () => {
    const fetchMock = vi.fn(async (path: string) =>
      path === "/api/backup/status" ? json({ status: "idle" }) : json({ data: [] }),
    );
    vi.stubGlobal("fetch", fetchMock);
    render(
      <QueryClientProvider client={new QueryClient()}>
        <Backup />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("Idle")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Trigger backup" }));
    expect(await screen.findByRole("alertdialog")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([p]) => p === "/api/backup/trigger")).toBe(false);
  });

  describe("after a manual trigger", () => {
    let status: "idle" | "running";
    let fetchMock: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      status = "idle";
      fetchMock = vi.fn(async (path: string, init?: RequestInit) => {
        if (path === "/api/backup/trigger" && init?.method === "POST") {
          status = "running";
          return new Response(JSON.stringify({ status: "started" }), {
            status: 202,
            headers: { "Content-Type": "application/json" },
          });
        }
        return path === "/api/backup/status" ? json({ status }) : json({ data: [] });
      });
      vi.stubGlobal("fetch", fetchMock);
    });

    afterEach(() => {
      vi.restoreAllMocks();
    });

    it("keeps the 5s status poll active until the run ends, then refetches snapshots", async () => {
      const setIntervalSpy = vi.spyOn(window, "setInterval");
      const clearIntervalSpy = vi.spyOn(window, "clearInterval");
      const client = new QueryClient();
      const tree = () => (
        <QueryClientProvider client={client}>
          <Backup />
        </QueryClientProvider>
      );
      const { rerender } = render(tree());
      expect(await screen.findByText("Idle")).toBeInTheDocument();

      await userEvent.click(screen.getByRole("button", { name: "Trigger backup" }));
      await userEvent.click(await screen.findByRole("button", { name: "Start backup" }));
      expect(await screen.findByText("Running")).toBeInTheDocument();

      rerender(tree());
      rerender(tree());

      const polls = setIntervalSpy.mock.calls
        .map(([, ms], i) => ({ ms, id: setIntervalSpy.mock.results[i].value }))
        .filter((c) => c.ms === 5000);
      expect(polls).toHaveLength(1);
      expect(clearIntervalSpy).not.toHaveBeenCalledWith(polls[0].id);

      // The run finishes: the next poll sees idle, polling stops, snapshots refetch.
      const snapshotCalls = () =>
        fetchMock.mock.calls.filter(([p]) => p === "/api/backup/snapshots").length;
      const before = snapshotCalls();
      status = "idle";
      (setIntervalSpy.mock.calls.find(([, ms]) => ms === 5000)![0] as () => void)();

      await waitFor(() => expect(screen.getByText("Idle")).toBeInTheDocument());
      await waitFor(() => expect(clearIntervalSpy).toHaveBeenCalledWith(polls[0].id));
      await waitFor(() => expect(snapshotCalls()).toBe(before + 1));
    });
  });
});
