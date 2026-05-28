import { useEffect, useRef, useState } from "react";

export type ConnectionStatus = "connecting" | "connected" | "disconnected";

export interface TagPoint {
  plc: string;
  tag: string;
  value: number | string | boolean | null;
  timestamp: string;
}

export interface UseTagStreamReturn {
  status: ConnectionStatus;
  tags: Map<string, TagPoint[]>;
}

interface ServerMessage {
  type: string;
  plc?: string;
  tag?: string;
  value?: TagPoint["value"];
  timestamp?: string;
}

const MAX_POINTS_PER_TAG = 200;
const BACKOFF_SEQUENCE_MS = [1000, 2000, 4000, 8000, 16000, 30000];

function tagKey(plc: string, tag: string): string {
  return `${plc}:${tag}`;
}

export function useTagStream(token: string | null): UseTagStreamReturn {
  const [status, setStatus] = useState<ConnectionStatus>(
    token ? "connecting" : "disconnected",
  );
  const [tags, setTags] = useState<Map<string, TagPoint[]>>(() => new Map());
  const socketRef = useRef<WebSocket | null>(null);
  const reconnectTimerRef = useRef<number | null>(null);
  const reconnectAttemptRef = useRef(0);
  const intentionallyClosedRef = useRef(false);

  useEffect(() => {
    if (!token) {
      setStatus("disconnected");
      return;
    }

    intentionallyClosedRef.current = false;

    const connect = () => {
      const proto = window.location.protocol === "https:" ? "wss" : "ws";
      const url = `${proto}://${window.location.host}/api/ws/tags?token=${encodeURIComponent(token)}`;
      const ws = new WebSocket(url);
      socketRef.current = ws;
      setStatus("connecting");

      ws.onopen = () => {
        reconnectAttemptRef.current = 0;
      };

      ws.onmessage = (ev) => {
        let msg: ServerMessage;
        try {
          msg = JSON.parse(ev.data) as ServerMessage;
        } catch {
          return;
        }
        switch (msg.type) {
          case "subscribed":
            setStatus("connected");
            return;
          case "tag_update": {
            if (!msg.plc || !msg.tag || !msg.timestamp) return;
            const point: TagPoint = {
              plc: msg.plc,
              tag: msg.tag,
              value: msg.value ?? null,
              timestamp: msg.timestamp,
            };
            setTags((prev) => {
              const next = new Map(prev);
              const key = tagKey(msg.plc!, msg.tag!);
              const buffer = next.get(key) ?? [];
              const updated = [...buffer, point];
              if (updated.length > MAX_POINTS_PER_TAG) {
                updated.splice(0, updated.length - MAX_POINTS_PER_TAG);
              }
              next.set(key, updated);
              return next;
            });
            return;
          }
          case "ping":
          case "pong":
          case "unsubscribed":
            return;
          default:
            return;
        }
      };

      ws.onerror = () => {
        // ignore — onclose handles reconnect
      };

      ws.onclose = (ev) => {
        socketRef.current = null;
        if (intentionallyClosedRef.current || ev.code === 1000) {
          setStatus("disconnected");
          return;
        }
        setStatus("disconnected");
        const attempt = reconnectAttemptRef.current;
        const delay =
          BACKOFF_SEQUENCE_MS[Math.min(attempt, BACKOFF_SEQUENCE_MS.length - 1)];
        reconnectAttemptRef.current = attempt + 1;
        reconnectTimerRef.current = window.setTimeout(connect, delay);
      };
    };

    connect();

    return () => {
      intentionallyClosedRef.current = true;
      if (reconnectTimerRef.current !== null) {
        window.clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      const ws = socketRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.close(1000, "client unmount");
      } else if (ws) {
        ws.close();
      }
      socketRef.current = null;
    };
  }, [token]);

  return { status, tags };
}
