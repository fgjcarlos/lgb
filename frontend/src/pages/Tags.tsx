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
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { useCurrentTags } from "@/hooks/useApi";

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
          Configured PLC → tag mapping definitions.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <UnavailableBanner message="Tag mapping config endpoint is not implemented in this release. Configure mappings via the gateway YAML config and restart the service." />
      </CardContent>
    </Card>
  );
}
