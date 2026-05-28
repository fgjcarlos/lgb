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
import { Badge } from "@/components/ui/badge";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { useDoctorChecks, type DoctorCheck } from "@/hooks/useApi";
import { ApiError } from "@/lib/api";

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
            Aggregated health checks from the gateway doctor.
          </p>
        </div>
        {query.data && overallBadge(query.data.overall)}
      </header>

      {unavailable && (
        <UnavailableBanner message="Doctor checks are not exposed by this gateway build. Ensure the doctor endpoint is enabled and rebuild." />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Checks</CardTitle>
          <CardDescription>
            Each registered doctor.Check contributes to the overall severity.
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
