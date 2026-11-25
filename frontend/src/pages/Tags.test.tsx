import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTagWriteMetaMap, WriteControl } from "@/pages/Tags";

vi.mock("@/contexts/auth", () => ({
  useAuth: () => ({ getWsToken: () => "tok" }),
}));

const fetchMock = vi.fn();

function renderControl(type = "Int16") {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <WriteControl plc="line 1" tag="speed" writable type={type} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useTagWriteMetaMap", () => {
  it("maps plc|tag to write meta and keeps the same Map across re-renders", async () => {
    fetchMock.mockImplementation(
      async () =>
        new Response(
          JSON.stringify({
            data: [{ name: "p1", tags: [{ name: "t1", writable: true, type: "Int16" }] }],
          }),
        ),
    );
    const client = new QueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result, rerender } = renderHook(() => useTagWriteMetaMap(), { wrapper });

    await waitFor(() => expect(result.current.size).toBe(1));
    expect(result.current.get("p1|t1")).toEqual({ writable: true, type: "Int16" });

    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });
});

describe("WriteControl", () => {
  it("shows permission denied on 403 without the generic error", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: "forbidden", message: "tag write not permitted" } }),
        { status: 403 },
      ),
    );
    renderControl();

    await userEvent.type(screen.getByLabelText("Value for speed"), "42");
    await userEvent.click(screen.getByRole("button", { name: "Write" }));

    expect(await screen.findByText("permission denied")).toBeInTheDocument();
    expect(screen.queryByText("tag write not permitted")).not.toBeInTheDocument();
    expect(screen.queryByText("sent")).not.toBeInTheDocument();
    // A 403 keeps the typed value so the operator sees what was refused.
    expect(screen.getByLabelText("Value for speed")).toHaveValue(42);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [path, init] = fetchMock.mock.calls[0];
    expect(path).toBe("/api/plcs/line%201/tags/speed/write");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ value: 42 });
  });

  it("shows sent and clears the input on success", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    renderControl();

    await userEvent.type(screen.getByLabelText("Value for speed"), "7");
    await userEvent.click(screen.getByRole("button", { name: "Write" }));

    expect(await screen.findByText("sent")).toBeInTheDocument();
    expect(screen.getByLabelText("Value for speed")).toHaveValue(null);
  });

  it("rejects unrepresentable input without calling the API", async () => {
    renderControl("Int64");
    await userEvent.type(screen.getByLabelText("Value for speed"), "9007199254740993");
    await userEvent.click(screen.getByRole("button", { name: "Write" }));

    expect(screen.getByRole("alert")).toHaveTextContent(
      "integers beyond ±9007199254740991 cannot be written from the UI",
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("renders read-only when the tag is not writable", () => {
    const client = new QueryClient();
    render(
      <QueryClientProvider client={client}>
        <WriteControl plc="p" tag="t" writable={false} type="Int16" />
      </QueryClientProvider>,
    );
    expect(screen.getByText("read-only")).toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
