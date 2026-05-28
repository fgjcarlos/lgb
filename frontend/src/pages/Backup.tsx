import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HardDriveDownload, RefreshCw } from "lucide-react";
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
import { useAuth } from "@/contexts/auth";
import { apiFetch, ApiError } from "@/lib/api";
import { useBackupStatus, type BackupStatusResponse } from "@/hooks/useApi";

interface Snapshot {
  id: string;
  time: string;
  hostname: string;
  paths: string[];
}

interface SnapshotsResponse {
  data: Snapshot[];
}

function statusBadge(status: BackupStatusResponse["status"]) {
  if (status === "running") {
    return <Badge variant="warning">Running</Badge>;
  }
  if (status === "failed") {
    return <Badge variant="destructive">Failed</Badge>;
  }
  return <Badge variant="secondary">Idle</Badge>;
}

export function Backup() {
  const { token } = useAuth();
  const queryClient = useQueryClient();
  const [polling, setPolling] = useState(false);
  const [triggerError, setTriggerError] = useState<string | null>(null);

  const statusQuery = useBackupStatus(true);
  const snapshotsQuery = useQuery({
    queryKey: ["backup", "snapshots"],
    queryFn: () => apiFetch<SnapshotsResponse>("/api/backup/snapshots", { token }),
    enabled: !!token,
  });

  const trigger = useMutation({
    mutationFn: () =>
      apiFetch<{ status: string }>("/api/backup/trigger", {
        method: "POST",
        token,
      }),
    onSuccess: () => {
      setTriggerError(null);
      setPolling(true);
      queryClient.invalidateQueries({ queryKey: ["backup", "status"] });
    },
    onError: (err) => {
      const message = err instanceof Error ? err.message : "trigger failed";
      setTriggerError(message);
    },
  });

  // Stop polling once status returns to idle/failed; refresh snapshots.
  useEffect(() => {
    if (!polling) return;
    const status = statusQuery.data?.status;
    if (status === "idle" || status === "failed") {
      setPolling(false);
      queryClient.invalidateQueries({ queryKey: ["backup", "snapshots"] });
    }
  }, [polling, statusQuery.data?.status, queryClient]);

  // While polling, refetch every 5s.
  useEffect(() => {
    if (!polling) return;
    const id = window.setInterval(() => {
      statusQuery.refetch();
    }, 5000);
    return () => window.clearInterval(id);
  }, [polling, statusQuery]);

  const unavailable =
    (statusQuery.error instanceof ApiError &&
      (statusQuery.error.status === 404 || statusQuery.error.status === 503)) ||
    (snapshotsQuery.error instanceof ApiError &&
      (snapshotsQuery.error.status === 404 ||
        snapshotsQuery.error.status === 503));

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Backup</h1>
          <p className="text-sm text-muted-foreground">
            Trigger backups and review snapshots in the configured restic repository.
          </p>
        </div>
        {statusQuery.data && statusBadge(statusQuery.data.status)}
      </header>

      {unavailable && (
        <UnavailableBanner message="Backup manager is not configured on this gateway. Enable backup repositories in the YAML config and restart the service." />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Run a backup</CardTitle>
          <CardDescription>
            Backups run asynchronously. The status polls every 5 s while a run
            is in progress, then refreshes the snapshot list.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center gap-2">
            <Button
              onClick={() => trigger.mutate()}
              disabled={
                trigger.isPending ||
                polling ||
                statusQuery.data?.status === "running"
              }
            >
              <HardDriveDownload className="mr-2 h-4 w-4" />
              {polling || statusQuery.data?.status === "running"
                ? "Running…"
                : "Trigger backup"}
            </Button>
            <Button
              variant="outline"
              size="icon"
              onClick={() => {
                statusQuery.refetch();
                snapshotsQuery.refetch();
              }}
              aria-label="Refresh"
            >
              <RefreshCw className="h-4 w-4" />
            </Button>
          </div>
          {triggerError && (
            <p
              role="alert"
              className="text-sm text-destructive"
            >
              {triggerError}
            </p>
          )}
          {statusQuery.data?.last_run && (
            <p className="text-xs text-muted-foreground">
              Last run:{" "}
              {new Date(statusQuery.data.last_run).toLocaleString()}
              {statusQuery.data.last_error
                ? ` — ${statusQuery.data.last_error}`
                : ""}
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Snapshots</CardTitle>
          <CardDescription>
            Listed from the first configured restic repository.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {snapshotsQuery.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : snapshotsQuery.isError && !unavailable ? (
            <p className="text-sm text-destructive">
              {snapshotsQuery.error.message}
            </p>
          ) : !snapshotsQuery.data || snapshotsQuery.data.data.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No snapshots in the repository yet.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Time</TableHead>
                  <TableHead>Hostname</TableHead>
                  <TableHead>Paths</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {snapshotsQuery.data.data.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell className="font-mono text-xs">
                      {s.id.slice(0, 8)}
                    </TableCell>
                    <TableCell className="text-sm">
                      {new Date(s.time).toLocaleString()}
                    </TableCell>
                    <TableCell>{s.hostname}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {s.paths.join(", ")}
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
