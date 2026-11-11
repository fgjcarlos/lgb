import { useMemo } from "react";
import { Link, useParams } from "react-router-dom";
import { AlertTriangle, ArrowLeft } from "lucide-react";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ConnectionBadge } from "@/components/ConnectionBadge";
import {
  PLCStateBadge,
  QualityBadge,
  ScanHealthBadge,
} from "@/components/PLCStatusBadges";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import {
  useCurrentTags,
  useMappings,
  usePLCStatus,
  type Mapping,
  type PLCStatus as PLCStatusData,
} from "@/hooks/useApi";
import { useTagStream } from "@/hooks/useTagStream";
import { useAuth } from "@/contexts/auth";
import { ApiError } from "@/lib/api";

function formatTime(ts: string | null): string {
  return ts ? new Date(ts).toLocaleString() : "never";
}

function formatAge(ts: string | null): string {
  if (!ts) return "never";
  const seconds = Math.max(0, (Date.now() - new Date(ts).getTime()) / 1000);
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 1 : 0)} s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min ago`;
  return new Date(ts).toLocaleString();
}

function errorRate(st: PLCStatusData): string {
  if (st.read_count === 0) return "—";
  return `${((st.read_error_count / st.read_count) * 100).toFixed(1)} %`;
}

// ─── Overview: every configured PLC with its live state ────────────────────

function PLCStatusRow({ mapping }: { mapping: Mapping }) {
  const status = usePLCStatus(mapping.plc);
  const st = status.data?.data;
  return (
    <TableRow>
      <TableCell className="font-medium">
        <Link
          to={`/plcs/${encodeURIComponent(mapping.plc)}/status`}
          className="underline-offset-4 hover:underline"
        >
          {mapping.plc}
        </Link>
      </TableCell>
      <TableCell className="font-mono text-xs">{mapping.address}</TableCell>
      <TableCell>
        {st ? (
          <PLCStateBadge state={st.state} />
        ) : status.isError ? (
          <span className="text-xs text-muted-foreground">
            {status.error.message}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">Loading…</span>
        )}
      </TableCell>
      <TableCell>{st ? <ScanHealthBadge health={st.scan_health} /> : "—"}</TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {st ? formatAge(st.last_scan) : "—"}
      </TableCell>
      <TableCell>{st ? errorRate(st) : "—"}</TableCell>
    </TableRow>
  );
}

export function PLCStatusOverview() {
  const mappings = useMappings();
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">PLC Status</h1>
        <p className="text-sm text-muted-foreground">
          Connection state and scan health of every configured PLC.
        </p>
      </header>
      <Card>
        <CardContent className="pt-6">
          {mappings.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : mappings.isError ? (
            <p className="text-sm text-destructive">
              Failed to load PLCs: {mappings.error.message}
            </p>
          ) : !mappings.data || mappings.data.data.length === 0 ? (
            <p className="text-sm text-muted-foreground">No PLCs configured.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>PLC</TableHead>
                  <TableHead>Address</TableHead>
                  <TableHead>Connection</TableHead>
                  <TableHead>Scan</TableHead>
                  <TableHead>Last scan</TableHead>
                  <TableHead>Read error rate</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {mappings.data.data.map((m) => (
                  <PLCStatusRow key={m.plc} mapping={m} />
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ─── Detail: one PLC's status and live tags ────────────────────────────────

function ConnectionNotice({ st }: { st: PLCStatusData }) {
  if (st.state === "connected") return null;
  const text =
    st.state === "never_connected"
      ? "The gateway has not connected to this PLC yet. It keeps retrying with backoff (up to 30 s between attempts); tag values appear once the first scan completes."
      : "The connection to this PLC was lost. The gateway is reconnecting; tag values below are the last ones read.";
  return (
    <div
      role="status"
      className="flex items-start gap-3 rounded-md border border-amber-300 bg-amber-50 p-4 text-amber-900 dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100"
    >
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
      <div className="space-y-1 text-sm">
        <p>{text}</p>
        {st.last_error && (
          <p className="font-mono text-xs">
            Last error ({formatTime(st.last_error_at)}): {st.last_error}
          </p>
        )}
      </div>
    </div>
  );
}

function Stat({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 py-1 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right">{children}</span>
    </div>
  );
}

interface LiveTagRow {
  tag: string;
  type: string;
  value: unknown;
  quality: string | undefined;
  timestamp: string | null;
}

function useLiveTags(plc: string, mapping: Mapping | undefined): {
  rows: LiveTagRow[];
  feed: ReturnType<typeof useTagStream>["status"];
} {
  const { getWsToken } = useAuth();
  const { status: feed, tags: live } = useTagStream(getWsToken);
  const snapshot = useCurrentTags({ limit: 1000 });

  const rows = useMemo<LiveTagRow[]>(() => {
    const byTag = new Map<string, LiveTagRow>();
    for (const t of mapping?.tags ?? []) {
      byTag.set(t.name, { tag: t.name, type: t.type, value: undefined, quality: undefined, timestamp: null });
    }
    const merge = (tag: string, value: unknown, quality: string | undefined, timestamp: string) => {
      const prev = byTag.get(tag);
      if (prev?.timestamp && new Date(prev.timestamp) > new Date(timestamp)) return;
      byTag.set(tag, {
        tag,
        type: prev?.type ?? "",
        value,
        quality: quality || prev?.quality,
        timestamp,
      });
    };
    for (const row of snapshot.data?.data ?? []) {
      if (row.plc === plc) merge(row.tag, row.value, row.quality, row.timestamp);
    }
    live.forEach((buffer) => {
      const last = buffer[buffer.length - 1];
      if (last && last.plc === plc) merge(last.tag, last.value, last.quality, last.timestamp);
    });
    return [...byTag.values()].sort((a, b) => a.tag.localeCompare(b.tag));
  }, [plc, mapping, snapshot.data, live]);

  return { rows, feed };
}

export function PLCStatusDetail() {
  const { name = "" } = useParams<{ name: string }>();
  const status = usePLCStatus(name);
  const mappings = useMappings();
  const mapping = mappings.data?.data.find((m) => m.plc === name);
  const { rows, feed } = useLiveTags(name, mapping);
  const st = status.data?.data;
  const notFound = status.error instanceof ApiError && status.error.status === 404;
  const unavailable = status.error instanceof ApiError && status.error.status === 503;

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between gap-4">
        <div>
          <Link
            to="/plc-status"
            className="mb-1 inline-flex items-center gap-1 text-xs text-muted-foreground hover:underline"
          >
            <ArrowLeft className="h-3 w-3" /> All PLCs
          </Link>
          <h1 className="text-2xl font-semibold">{name}</h1>
          <p className="text-sm text-muted-foreground">
            {mapping ? `${mapping.address} · scan rate ${mapping.scan_rate || "1s"}` : "PLC status"}
          </p>
        </div>
        {st && <PLCStateBadge state={st.state} />}
      </header>

      {status.isLoading ? (
        <p className="text-sm text-muted-foreground">Loading status…</p>
      ) : notFound ? (
        <Card>
          <CardHeader>
            <CardTitle>PLC not found</CardTitle>
            <CardDescription>
              “{name}” is not configured or is not running on this gateway.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : unavailable ? (
        <UnavailableBanner message="PLC status is not available on this gateway." />
      ) : status.isError ? (
        <p className="text-sm text-destructive">
          Failed to load status: {status.error.message}
        </p>
      ) : st ? (
        <>
          <ConnectionNotice st={st} />
          <div className="grid gap-4 md:grid-cols-3">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Connection</CardTitle>
              </CardHeader>
              <CardContent>
                <Stat label="State"><PLCStateBadge state={st.state} /></Stat>
                <Stat label="Connected since">{formatTime(st.connected_since)}</Stat>
                <Stat label="Connect errors">{st.connect_error_count}</Stat>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Scan</CardTitle>
              </CardHeader>
              <CardContent>
                <Stat label="Health"><ScanHealthBadge health={st.scan_health} /></Stat>
                <Stat label="Last scan">{formatAge(st.last_scan)}</Stat>
                <Stat label="Last scan took">
                  {st.last_scan_duration_ms === null ? "—" : `${st.last_scan_duration_ms.toFixed(1)} ms`}
                </Stat>
                <Stat label="Scan rate">{st.scan_rate}</Stat>
                <Stat label="Scans">{st.scan_count}</Stat>
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Errors</CardTitle>
              </CardHeader>
              <CardContent>
                <Stat label="Read error rate">{errorRate(st)}</Stat>
                <Stat label="Read errors">
                  {st.read_error_count} / {st.read_count}
                </Stat>
                <Stat label="Last error">
                  {st.last_error ? (
                    <span className="font-mono text-xs" title={st.last_error}>
                      {formatAge(st.last_error_at)}
                    </span>
                  ) : (
                    "none"
                  )}
                </Stat>
              </CardContent>
            </Card>
          </div>
        </>
      ) : null}

      {!notFound && (
        <Card>
          <CardHeader className="flex flex-row items-start justify-between gap-4">
            <div>
              <CardTitle>Tags</CardTitle>
              <CardDescription>
                Live values from the realtime feed, seeded from the current snapshot.
              </CardDescription>
            </div>
            <ConnectionBadge status={feed} />
          </CardHeader>
          <CardContent>
            {rows.length === 0 ? (
              <p className="text-sm text-muted-foreground">No tags configured for this PLC.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Tag</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Quality</TableHead>
                    <TableHead>Timestamp</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((r) => (
                    <TableRow key={r.tag}>
                      <TableCell className="font-mono text-xs">{r.tag}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{r.type || "—"}</TableCell>
                      <TableCell>
                        {r.timestamp === null ? (
                          <span className="text-muted-foreground">no data</span>
                        ) : (
                          String(r.value ?? "—")
                        )}
                      </TableCell>
                      <TableCell>
                        <QualityBadge quality={r.timestamp === null ? undefined : r.quality ?? "good"} />
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {r.timestamp ? new Date(r.timestamp).toLocaleString() : "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
