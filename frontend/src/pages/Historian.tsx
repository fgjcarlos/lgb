import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
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
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import {
  useHistorianQuery,
  type HistorianQueryParams,
} from "@/hooks/useApi";
import { ApiError } from "@/lib/api";

const querySchema = z.object({
  plc: z.string().optional(),
  tag: z.string().min(1, "Tag is required"),
  from: z.string().optional(),
  to: z.string().optional(),
  limit: z.coerce.number().int().min(1).max(1000).optional(),
});

type QueryFormValues = z.infer<typeof querySchema>;

function toNumeric(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "boolean") return value ? 1 : 0;
  if (typeof value === "string") {
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

function toIsoOrUndef(local: string | undefined): string | undefined {
  if (!local) return undefined;
  const d = new Date(local);
  if (Number.isNaN(d.getTime())) return undefined;
  return d.toISOString();
}

export function Historian() {
  const [params, setParams] = useState<HistorianQueryParams | null>(null);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<QueryFormValues>({
    resolver: zodResolver(querySchema),
    defaultValues: { plc: "", tag: "", from: "", to: "", limit: 100 },
  });

  const query = useHistorianQuery(params);

  const onSubmit = handleSubmit((values) => {
    setParams({
      plc: values.plc || undefined,
      tag: values.tag,
      from: toIsoOrUndef(values.from),
      to: toIsoOrUndef(values.to),
      limit: values.limit,
    });
  });

  const endpointUnavailable =
    query.error instanceof ApiError && query.error.status === 503;

  const chartPoints =
    query.data?.data
      .map((row) => {
        const numeric = toNumeric(row.value);
        if (numeric === null) return null;
        return { t: new Date(row.timestamp).getTime(), value: numeric };
      })
      .filter((p): p is { t: number; value: number } => p !== null) ?? [];

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Historian</h1>
        <p className="text-sm text-muted-foreground">
          Query stored PLC tag samples over a time range.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Query</CardTitle>
          <CardDescription>
            Tag is required. From / to default to all-time when blank.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="grid gap-4 md:grid-cols-2 lg:grid-cols-5"
            onSubmit={onSubmit}
          >
            <div className="space-y-2">
              <Label htmlFor="plc">PLC</Label>
              <Input id="plc" placeholder="all" {...register("plc")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="tag">Tag *</Label>
              <Input id="tag" {...register("tag")} />
              {errors.tag && (
                <p className="text-xs text-destructive">{errors.tag.message}</p>
              )}
            </div>
            <div className="space-y-2">
              <Label htmlFor="from">From</Label>
              <Input
                id="from"
                type="datetime-local"
                {...register("from")}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="to">To</Label>
              <Input id="to" type="datetime-local" {...register("to")} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="limit">Limit</Label>
              <Input
                id="limit"
                type="number"
                min={1}
                max={1000}
                {...register("limit")}
              />
              {errors.limit && (
                <p className="text-xs text-destructive">{errors.limit.message}</p>
              )}
            </div>
            <div className="md:col-span-2 lg:col-span-5">
              <Button type="submit" disabled={query.isFetching}>
                {query.isFetching ? "Querying…" : "Run query"}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      {endpointUnavailable && (
        <UnavailableBanner message="Historian store is not configured on this gateway. Enable it in the YAML config and restart the service." />
      )}

      {params && !endpointUnavailable && (
        <Card>
          <CardHeader>
            <CardTitle>Results</CardTitle>
            <CardDescription>
              {query.data
                ? `${query.data.data.length} sample${query.data.data.length === 1 ? "" : "s"}`
                : "Running…"}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {query.isLoading ? (
              <p className="text-sm text-muted-foreground">Loading…</p>
            ) : query.isError ? (
              <p className="text-sm text-destructive">
                {query.error.message}
              </p>
            ) : !query.data || query.data.data.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No historical data for the selected range.
              </p>
            ) : (
              <>
                {chartPoints.length > 0 && (
                  <div className="h-64">
                    <ResponsiveContainer width="100%" height="100%">
                      <LineChart data={chartPoints}>
                        <CartesianGrid strokeDasharray="3 3" />
                        <XAxis
                          dataKey="t"
                          type="number"
                          domain={["dataMin", "dataMax"]}
                          tickFormatter={(t) =>
                            new Date(t).toLocaleString()
                          }
                        />
                        <YAxis allowDecimals />
                        <Tooltip
                          labelFormatter={(t) =>
                            new Date(t).toLocaleString()
                          }
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
                  </div>
                )}
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
                    {query.data.data.map((row, i) => (
                      <TableRow key={`${row.plc_name}:${row.tag}:${i}`}>
                        <TableCell>{row.plc_name}</TableCell>
                        <TableCell className="font-mono text-xs">
                          {row.tag}
                        </TableCell>
                        <TableCell>{String(row.value)}</TableCell>
                        <TableCell>{row.quality}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {new Date(row.timestamp).toLocaleString()}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
