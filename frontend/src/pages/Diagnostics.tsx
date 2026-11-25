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
import { Link } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { PLCStateBadge, ScanHealthBadge } from "@/components/PLCStatusBadges";
import {
  useDoctorChecks,
  usePLCDiagnostics,
  type DoctorCheck,
  type PLCStatus,
} from "@/hooks/useApi";
import { ApiError } from "@/lib/api";

function ms(value: number | null): string {
  return value === null ? "—" : value.toFixed(1);
}

function withErrors(total: number, errors: number) {
  return (
    <>
      {total}
      {errors > 0 && <span className="text-destructive"> ({errors} err)</span>}
    </>
  );
}

function PLCDiagnosticsCard() {
  const query = usePLCDiagnostics();
  const unavailable =
    query.error instanceof ApiError &&
    (query.error.status === 404 || query.error.status === 503);
  // On error, do not keep showing the last successful snapshot as if current.
  const plcs: PLCStatus[] | undefined = query.isError ? undefined : query.data?.data;

  return (
    <Card>
      <CardHeader>
        <CardTitle>PLC runtime</CardTitle>
        <CardDescription>
          Live connection state, scan latency and error counters, refreshed
          every 5 s. Counters are cumulative until the gateway restarts or the
          PLC configuration changes. Latency covers the last 60 scans.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {unavailable ? (
          <p className="text-sm text-muted-foreground">
            PLC diagnostics are not exposed by this gateway build.
          </p>
        ) : query.isLoading ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : query.isError ? (
          <p className="text-sm text-destructive">{query.error.message}</p>
        ) : !plcs || plcs.length === 0 ? (
          <p className="text-sm text-muted-foreground">No PLCs are configured.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>PLC</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Scan</TableHead>
                <TableHead>Last scan</TableHead>
                <TableHead className="text-right">Latency avg / max (ms)</TableHead>
                <TableHead className="text-right">Reads</TableHead>
                <TableHead className="text-right">Writes</TableHead>
                <TableHead className="text-right">Connect errors</TableHead>
                <TableHead>Last error</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {plcs.map((p) => (
                <TableRow key={p.name}>
                  <TableCell className="font-medium">
                    <Link
                      to={`/plcs/${encodeURIComponent(p.name)}/status`}
                      className="underline-offset-4 hover:underline"
                    >
                      {p.name}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <PLCStateBadge state={p.state} />
                  </TableCell>
                  <TableCell>
                    <ScanHealthBadge health={p.scan_health} />
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {p.last_scan ? new Date(p.last_scan).toLocaleString() : "never"}
                  </TableCell>
                  <TableCell className="text-right font-mono text-xs">
                    {ms(p.scan_latency_avg_ms)} / {ms(p.scan_latency_max_ms)}
                  </TableCell>
                  <TableCell className="text-right">
                    {withErrors(p.read_count, p.read_error_count)}
                  </TableCell>
                  <TableCell className="text-right">
                    {withErrors(p.write_count, p.write_error_count)}
                  </TableCell>
                  <TableCell className="text-right">{p.connect_error_count}</TableCell>
                  <TableCell className="max-w-xs truncate text-sm text-muted-foreground" title={p.last_error}>
                    {p.last_error || "—"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function statusBadge(status: DoctorCheck["status"]) {
  if (status === "pass") return <Badge variant="success">pass</Badge>;
  if (status === "warn") return <Badge variant="warning">warn</Badge>;
  return <Badge variant="destructive">fail</Badge>;
}

function overallBadge(status: DoctorCheck["status"]) {
  if (status === "pass") {
    return <Badge variant="success">Healthy</Badge>;
  }
  if (status === "warn") {
    return <Badge variant="warning">Degraded</Badge>;
  }
  return <Badge variant="destructive">Failing</Badge>;
}

export function Diagnostics() {
  const query = useDoctorChecks();

  const unavailable =
    query.error instanceof ApiError &&
    (query.error.status === 404 || query.error.status === 503);

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Diagnostics</h1>
          <p className="text-sm text-muted-foreground">
            Live PLC runtime state and the gateway doctor checks.
          </p>
        </div>
        {query.data && overallBadge(query.data.overall)}
      </header>

      {unavailable && (
        <UnavailableBanner message="Doctor checks are not exposed by this gateway build. Ensure the doctor endpoint is enabled and rebuild." />
      )}

      <PLCDiagnosticsCard />

      <Card>
        <CardHeader>
          <CardTitle>Checks</CardTitle>
          <CardDescription>
            Each registered doctor.Check contributes to the overall severity.
            Refreshed every 10 s.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {query.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : query.isError && !unavailable ? (
            <p className="text-sm text-destructive">{query.error.message}</p>
          ) : !query.data || query.data.checks.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No checks are currently registered.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Message</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {query.data.checks.map((check) => (
                  <TableRow key={check.name}>
                    <TableCell className="font-medium">{check.name}</TableCell>
                    <TableCell>{statusBadge(check.status)}</TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {check.message || "—"}
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
