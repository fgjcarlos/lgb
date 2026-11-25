import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useTagStream } from "@/hooks/useTagStream";

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readyState = FakeWebSocket.CONNECTING;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  close = vi.fn();

  constructor(public url: string) {
    FakeWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }

  open() {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  drop(code = 1006) {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.({ code });
  }
}

const sockets = () => FakeWebSocket.instances;
const last = () => sockets()[sockets().length - 1];
const getToken = () => "tok";

beforeEach(() => {
  vi.useFakeTimers();
  FakeWebSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeWebSocket);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("useTagStream", () => {
  it("authenticates on open and is connected after subscribed", () => {
    const { result } = renderHook(() => useTagStream(getToken));
    expect(result.current.status).toBe("connecting");
    expect(last().url).toBe(`ws://${window.location.host}/api/ws/tags`);

    act(() => last().open());
    expect(last().sent).toEqual([JSON.stringify({ type: "auth", token: "tok" })]);

    act(() => last().message({ type: "subscribed" }));
    expect(result.current.status).toBe("connected");

    act(() =>
      last().message({ type: "tag_update", plc: "p", tag: "t", value: 3, timestamp: "ts" }),
    );
    expect(result.current.tags.get("p:t")).toEqual([
      { plc: "p", tag: "t", value: 3, quality: undefined, timestamp: "ts" },
    ]);
  });

  it("does not connect without a token", () => {
    const { result } = renderHook(() => useTagStream(() => null));
    expect(result.current.status).toBe("disconnected");
    expect(sockets()).toHaveLength(0);
  });

  it("reconnects with exponential backoff capped at 30s", () => {
    const { result } = renderHook(() => useTagStream(getToken));
    const expected = [1000, 2000, 4000, 8000, 16000, 30000, 30000];

    for (const delay of expected) {
      const count = sockets().length;
      act(() => last().drop());
      expect(result.current.status).toBe("disconnected");

      act(() => vi.advanceTimersByTime(delay - 1));
      expect(sockets()).toHaveLength(count);
      act(() => vi.advanceTimersByTime(1));
      expect(sockets()).toHaveLength(count + 1);
    }
  });

  it("resets the backoff after a successful open", () => {
    renderHook(() => useTagStream(getToken));
    act(() => last().drop());
    act(() => vi.advanceTimersByTime(1000));
    act(() => last().drop());
    act(() => vi.advanceTimersByTime(2000));
    expect(sockets()).toHaveLength(3);

    act(() => last().open());
    act(() => last().drop());
    act(() => vi.advanceTimersByTime(1000));
    expect(sockets()).toHaveLength(4);
  });

  it("does not reconnect after a normal close", () => {
    renderHook(() => useTagStream(getToken));
    act(() => last().drop(1000));
    act(() => vi.advanceTimersByTime(60_000));
    expect(sockets()).toHaveLength(1);
  });

  it("closes the socket and cancels a pending reconnect on unmount", () => {
    const { unmount } = renderHook(() => useTagStream(getToken));
    act(() => last().drop());
    unmount();
    act(() => vi.advanceTimersByTime(60_000));
    expect(sockets()).toHaveLength(1);

    const second = renderHook(() => useTagStream(getToken));
    act(() => last().open());
    second.unmount();
    expect(last().close).toHaveBeenCalledWith(1000, "client unmount");
  });
});
