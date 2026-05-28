import { useMemo } from "react";
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
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
import { useTagStream } from "@/hooks/useTagStream";
import { useCurrentTags } from "@/hooks/useApi";
import { useAuth } from "@/contexts/auth";

interface ChartSeries {
  key: string;
  plc: string;
  tag: string;
  points: { t: number; value: number }[];
  latest: { value: unknown; timestamp: string } | null;
}

function toNumeric(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "boolean") return value ? 1 : 0;
  if (typeof value === "string") {
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

export function Dashboard() {
  const { token } = useAuth();
  const { status, tags: liveTags } = useTagStream(token);
  const snapshot = useCurrentTags({ limit: 100 });

  const series = useMemo<ChartSeries[]>(() => {
    const out: ChartSeries[] = [];
    liveTags.forEach((buffer, key) => {
      if (buffer.length === 0) return;
      const last = buffer[buffer.length - 1];
      const points = buffer
        .map((p) => {
          const numeric = toNumeric(p.value);
          if (numeric === null) return null;
          return { t: new Date(p.timestamp).getTime(), value: numeric };
        })
        .filter((p): p is { t: number; value: number } => p !== null);
      if (points.length === 0) return;
      out.push({
        key,
        plc: last.plc,
        tag: last.tag,
        points,
        latest: { value: last.value, timestamp: last.timestamp },
      });
    });
    return out.sort((a, b) => a.key.localeCompare(b.key));
  }, [liveTags]);

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Dashboard</h1>
          <p className="text-sm text-muted-foreground">
            Live PLC tag values and rolling history.
          </p>
        </div>
        <ConnectionBadge status={status} />
      </header>

      {series.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>Waiting for tag updates…</CardTitle>
            <CardDescription>
              Charts appear once the gateway streams numeric tag values.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {series.map((s) => (
            <Card key={s.key}>
              <CardHeader>
                <CardTitle className="text-base">
                  {s.plc} / {s.tag}
                </CardTitle>
                <CardDescription>
                  Latest: {String(s.latest?.value ?? "—")}
                </CardDescription>
              </CardHeader>
              <CardContent className="h-48">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={s.points}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis
                      dataKey="t"
                      type="number"
                      domain={["dataMin", "dataMax"]}
                      tickFormatter={(t) => new Date(t).toLocaleTimeString()}
                    />
                    <YAxis allowDecimals />
                    <Tooltip
                      labelFormatter={(t) => new Date(t).toLocaleTimeString()}
                      formatter={(v) => [String(v), "value"]}
                    />
                    <Line
                      type="monotone"
                      dataKey="value"
                      stroke="hsl(var(--primary))"
                      strokeWidth={2}
                      dot={false}
                      isAnimationActive={false}
                    />
                  </LineChart>
                </ResponsiveContainer>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Current tag values</CardTitle>
          <CardDescription>
            Snapshot from GET /api/tags/current — refreshes every 10s.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {snapshot.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : snapshot.isError ? (
            <p className="text-sm text-destructive">
              Failed to load tags: {snapshot.error.message}
            </p>
          ) : !snapshot.data || snapshot.data.data.length === 0 ? (
            <p className="text-sm text-muted-foreground">No tags available.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>PLC</TableHead>
                  <TableHead>Tag</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead>Quality</TableHead>
                  <TableHead>Timestamp</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {snapshot.data.data.map((row) => (
                  <TableRow key={`${row.plc}:${row.tag}`}>
                    <TableCell>{row.plc}</TableCell>
                    <TableCell className="font-mono text-xs">{row.tag}</TableCell>
                    <TableCell>{String(row.value)}</TableCell>
                    <TableCell>{row.quality}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(row.timestamp).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
