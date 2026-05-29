import { useState } from "react";
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
import { Badge } from "@/components/ui/badge";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { useCurrentTags, useMappings } from "@/hooks/useApi";
import { ApiError } from "@/lib/api";

const PAGE_SIZE = 25;

export function Tags() {
  const [offset, setOffset] = useState(0);
  const tagsQuery = useCurrentTags({ limit: PAGE_SIZE, offset });

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
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.data.map((row) => (
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
