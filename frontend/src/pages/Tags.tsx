import { useState, useRef } from "react";
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
import { Badge } from "@/components/ui/badge";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { useCurrentTags, useMappings, usePLCs, useWriteTag } from "@/hooks/useApi";
import { ApiError } from "@/lib/api";

const PAGE_SIZE = 25;

// Build a fast writability lookup: "plcName|tagName" → true/false
function useWritabilityMap(): Map<string, boolean> {
  const plcsQuery = usePLCs();
  const map = new Map<string, boolean>();
  if (plcsQuery.data) {
    for (const plc of plcsQuery.data.data) {
      for (const tag of plc.tags) {
        map.set(`${plc.name}|${tag.name}`, tag.writable);
      }
    }
  }
  return map;
}

interface WriteControlProps {
  plc: string;
  tag: string;
  writable: boolean;
}

function WriteControl({ plc, tag, writable }: WriteControlProps) {
  const writeMutation = useWriteTag(plc, tag);
  const inputRef = useRef<HTMLInputElement>(null);

  if (!writable) {
    return (
      <span
        className="text-xs text-muted-foreground"
        title="Master write switch is off for this tag"
      >
        read-only
      </span>
    );
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const raw = inputRef.current?.value ?? "";
    if (raw === "") return;
    // Send as string; backend accepts any JSON value via `any`.
    writeMutation.mutate(
      { value: raw },
      {
        onSuccess: () => {
          if (inputRef.current) inputRef.current.value = "";
        },
      },
    );
  }

  const is403 =
    writeMutation.isError &&
    writeMutation.error instanceof ApiError &&
    writeMutation.error.status === 403;
  const is404 =
    writeMutation.isError &&
    writeMutation.error instanceof ApiError &&
    writeMutation.error.status === 404;
  const is400 =
    writeMutation.isError &&
    writeMutation.error instanceof ApiError &&
    writeMutation.error.status === 400;

  return (
    <form
      onSubmit={handleSubmit}
      className="flex items-center gap-1"
      aria-label={`Write value to ${tag}`}
    >
      <Input
        ref={inputRef}
        className="h-6 w-24 px-1 text-xs"
        placeholder="value"
        disabled={writeMutation.isPending}
      />
      <Button
        type="submit"
        size="sm"
        variant="outline"
        className="h-6 px-2 text-xs"
        disabled={writeMutation.isPending}
      >
        {writeMutation.isPending ? "…" : "Write"}
      </Button>
      {writeMutation.isSuccess && (
        <span className="text-xs text-green-600">sent</span>
      )}
      {is403 && (
        <span className="text-xs text-destructive">permission denied</span>
      )}
      {is404 && (
        <span className="text-xs text-destructive">tag not found</span>
      )}
      {is400 && (
        <span className="text-xs text-destructive">bad request</span>
      )}
      {writeMutation.isError && !is403 && !is404 && !is400 && (
        <span className="text-xs text-destructive">
          {writeMutation.error.message}
        </span>
      )}
    </form>
  );
}

export function Tags() {
  const [offset, setOffset] = useState(0);
  const tagsQuery = useCurrentTags({ limit: PAGE_SIZE, offset });
  const writabilityMap = useWritabilityMap();

  const data = tagsQuery.data;
  const count = data?.pagination.count ?? 0;
  const hasPrev = offset > 0;
  const hasNext = data ? offset + PAGE_SIZE < count : false;

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Tags</h1>
        <p className="text-sm text-muted-foreground">
          Current PLC tag inventory and most recent values.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Tag inventory</CardTitle>
          <CardDescription>
            {count > 0
              ? `Showing ${Math.min(offset + 1, count)}–${Math.min(offset + PAGE_SIZE, count)} of ${count}`
              : "Paginated GET /api/tags/current"}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {tagsQuery.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : tagsQuery.isError ? (
            <p className="text-sm text-destructive">
              Failed to load tags: {tagsQuery.error.message}
            </p>
          ) : !data || data.data.length === 0 ? (
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
                  <TableHead>Write</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.data.map((row) => {
                  const writable =
                    writabilityMap.get(`${row.plc}|${row.tag}`) ?? false;
                  return (
                    <TableRow key={`${row.plc}:${row.tag}`}>
                      <TableCell>{row.plc}</TableCell>
                      <TableCell className="font-mono text-xs">
                        {row.tag}
                      </TableCell>
                      <TableCell>{String(row.value)}</TableCell>
                      <TableCell>{row.quality}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {new Date(row.timestamp).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <WriteControl
                          plc={row.plc}
                          tag={row.tag}
                          writable={writable}
                        />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}

          <div className="flex items-center justify-between">
            <Button
              variant="outline"
              size="sm"
              disabled={!hasPrev}
              onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
            >
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={!hasNext}
              onClick={() => setOffset((o) => o + PAGE_SIZE)}
            >
              Next
            </Button>
          </div>
        </CardContent>
      </Card>

      <MappingSection />
    </div>
  );
}

function MappingSection() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Tag mappings</CardTitle>
        <CardDescription>
          Read-only view of the configured PLC → tag definitions. Writes are
          authored in the gateway YAML and hot-reloaded by the watcher.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <MappingsTable />
      </CardContent>
    </Card>
  );
}

function MappingsTable() {
  const query = useMappings();

  const unavailable =
    query.error instanceof ApiError &&
    (query.error.status === 404 || query.error.status === 503);

  if (unavailable) {
    return (
      <UnavailableBanner message="Mapping endpoint is not available on this gateway. Configure mappings via the YAML config." />
    );
  }
  if (query.isLoading) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (query.isError) {
    return (
      <p className="text-sm text-destructive">{query.error.message}</p>
    );
  }
  if (!query.data || query.data.data.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No PLC mappings configured.
      </p>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>PLC</TableHead>
          <TableHead>Address</TableHead>
          <TableHead>Scan rate</TableHead>
          <TableHead>Tags</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {query.data.data.map((m) => (
          <TableRow key={m.plc}>
            <TableCell className="font-medium">{m.plc}</TableCell>
            <TableCell className="font-mono text-xs">{m.address}</TableCell>
            <TableCell>{m.scan_rate}</TableCell>
            <TableCell>
              <div className="flex flex-wrap gap-1">
                {m.tags.length === 0 ? (
                  <span className="text-xs text-muted-foreground">
                    no tags
                  </span>
                ) : (
                  m.tags.map((t) => (
                    <Badge
                      key={t.name}
                      variant="outline"
                      title={t.type}
                      className="font-mono"
                    >
                      {t.name}
                      <span className="ml-1 text-[10px] text-muted-foreground">
                        :{t.type}
                      </span>
                    </Badge>
                  ))
                )}
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
