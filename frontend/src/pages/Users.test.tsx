import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Users } from "@/pages/Users";

vi.mock("@/contexts/auth", () => ({
  useAuth: () => ({ getWsToken: () => "tok", user: { id: 1, username: "root", role: "admin" } }),
}));

const json = (body: unknown) => new Response(JSON.stringify(body));
const bob = { id: 2, username: "bob", role: "admin", created_at: "2026-01-01T00:00:00Z" };

let fetchMock: ReturnType<typeof vi.fn>;
let total: number;

beforeEach(() => {
  total = 1;
  fetchMock = vi.fn(async (path: string, init?: RequestInit) => {
    if (init?.method === "PUT") {
      return path.endsWith("/password") ? new Response(null, { status: 204 }) : json({ data: bob });
    }
    if (path.startsWith("/api/users")) {
      const offset = Number(new URLSearchParams(path.split("?")[1]).get("offset"));
      return json({ data: [bob], pagination: { limit: 100, offset, count: total } });
    }
    return json({ data: [] });
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function renderPage() {
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { mutations: { retry: false } } })}>
      <Users />
    </QueryClientProvider>,
  );
}

const puts = () => fetchMock.mock.calls.filter(([, i]) => i?.method === "PUT");

describe("Users", () => {
  it("asks for confirmation before changing a role", async () => {
    renderPage();
    const select = await screen.findByRole("combobox", { name: "Role for bob" });

    await userEvent.selectOptions(select, "operator");
    expect(await screen.findByRole("alertdialog")).toHaveTextContent("Change bob's role to operator?");
    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(select).toHaveValue("admin");
    expect(puts()).toHaveLength(0);

    await userEvent.selectOptions(select, "operator");
    await userEvent.click(await screen.findByRole("button", { name: "Change role" }));
    await waitFor(() => expect(puts()).toHaveLength(1));
    const [path, init] = puts()[0];
    expect(path).toBe("/api/users/2/role");
    expect(JSON.parse(init.body)).toEqual({ role: "operator" });
  });

  it("lets an admin change a user's password", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Change password for bob" }));

    await userEvent.type(screen.getByLabelText("New password"), "correct-horse-1");
    await userEvent.type(screen.getByLabelText("Confirm password"), "correct-horse-2");
    await userEvent.click(screen.getByRole("button", { name: "Update password" }));
    expect(await screen.findByText("Passwords do not match")).toBeInTheDocument();
    expect(puts()).toHaveLength(0);

    await userEvent.clear(screen.getByLabelText("Confirm password"));
    await userEvent.type(screen.getByLabelText("Confirm password"), "correct-horse-1");
    await userEvent.click(screen.getByRole("button", { name: "Update password" }));

    expect(await screen.findByText("Password updated.")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    const [path, init] = puts()[0];
    expect(path).toBe("/api/users/2/password");
    expect(JSON.parse(init.body)).toEqual({ password: "correct-horse-1" });
  });

  it("pages through more users than one page holds", async () => {
    total = 150;
    renderPage();
    expect(await screen.findByText("Showing 1–100 of 150")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith("/api/users?limit=100&offset=100", expect.anything()),
    );
  });
});
