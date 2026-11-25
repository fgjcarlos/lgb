import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HardDriveDownload, HardDriveUpload, RefreshCw } from "lucide-react";
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
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { useAuth } from "@/contexts/auth";
import { apiFetch, ApiError } from "@/lib/api";
import {
  useBackupStatus,
  type BackupRestoreStatus,
  type BackupStatusResponse,
} from "@/hooks/useApi";

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

function restoreBadge(status: BackupRestoreStatus["status"]) {
  if (status === "running") {
    return <Badge variant="warning">Restoring</Badge>;
  }
  if (status === "failed") {
    return <Badge variant="destructive">Restore failed</Badge>;
  }
  if (status === "succeeded") {
    return <Badge variant="secondary">Restore succeeded</Badge>;
  }
  return null;
}

/**
 * Restores a snapshot into an empty directory. The live data directory is
 * never overwritten; the operator swaps files manually (docs/backup-restore.md).
 */
function RestoreDialog({
  snapshot,
  onClose,
  onStarted,
}: {
  snapshot: Snapshot | null;
  onClose: () => void;
  onStarted: () => void;
}) {
  const [destination, setDestination] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState<string | null>(null);
  const shortId = snapshot?.id.slice(0, 8) ?? "";

  useEffect(() => {
    setDestination("");
    setConfirmation("");
    setError(null);
  }, [snapshot]);

  const restore = useMutation({
    mutationFn: () =>
      apiFetch<{ status: string }>("/api/backup/restore", {
        method: "POST",
        body: JSON.stringify({
          repo: 0,
          snapshot_id: snapshot?.id,
          destination: destination.trim(),
        }),
      }),
    onSuccess: () => {
      onStarted();
      onClose();
    },
    onError: (err) =>
      setError(err instanceof Error ? err.message : "restore failed"),
  });

  const ready =
    destination.trim().startsWith("/") &&
    confirmation === shortId &&
    !restore.isPending;

  return (
    <AlertDialog open={!!snapshot} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Restore snapshot {shortId}?</AlertDialogTitle>
          <AlertDialogDescription>
            Files are restored into a new, empty directory. The live data
            directory is never overwritten: to put the restored data into
            service, stop the gateway and swap the files manually as described
            in the restore runbook.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <Label htmlFor="restore-destination">
              Destination (absolute path to a missing or empty directory)
            </Label>
            <Input
              id="restore-destination"
              placeholder="/var/lib/lgb-restore"
              value={destination}
              onChange={(e) => setDestination(e.target.value)}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="restore-confirm">
              Type <span className="font-mono">{shortId}</span> to confirm
            </Label>
            <Input
              id="restore-confirm"
              autoComplete="off"
              value={confirmation}
              onChange={(e) => setConfirmation(e.target.value)}
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={!ready}
            onClick={(e) => {
              // Keep the dialog open until the server accepts the request.
              e.preventDefault();
              restore.mutate();
            }}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {restore.isPending ? "Starting…" : "Restore"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function Backup() {
  const { getWsToken } = useAuth();
  const queryClient = useQueryClient();
  const [polling, setPolling] = useState(false);
  const [triggerError, setTriggerError] = useState<string | null>(null);
  const [restoreTarget, setRestoreTarget] = useState<Snapshot | null>(null);

  const statusQuery = useBackupStatus(true);
  // refetch is stable; the query object is not, so depending on it would
  // re-register the polling intervals on every render.
  const { refetch: refetchStatus } = statusQuery;
  const snapshotsQuery = useQuery({
    queryKey: ["backup", "snapshots"],
    queryFn: () => apiFetch<SnapshotsResponse>("/api/backup/snapshots", {}),
    enabled: !!getWsToken,
  });

  const trigger = useMutation({
    mutationFn: () =>
      apiFetch<{ status: string }>("/api/backup/trigger", {
        method: "POST",
        
      }),
    onSuccess: () => {
      setTriggerError(null);
      // The server is already running; the cached pre-trigger "idle" would
      // otherwise stop polling at once (#158).
      queryClient.setQueryData<BackupStatusResponse>(
        ["backup", "status"],
        (prev) => (prev ? { ...prev, status: "running" } : prev),
      );
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
      refetchStatus();
    }, 5000);
    return () => window.clearInterval(id);
  }, [polling, refetchStatus]);

  // While a restore runs, refetch every 2s until it succeeds or fails.
  const restoreStatus = statusQuery.data?.restore;
  const restoring = restoreStatus?.status === "running";
  useEffect(() => {
    if (!restoring) return;
    const id = window.setInterval(() => {
      refetchStatus();
    }, 2000);
    return () => window.clearInterval(id);
  }, [restoring, refetchStatus]);

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
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
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
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Start a backup now?</AlertDialogTitle>
                  <AlertDialogDescription>
                    The gateway takes a snapshot with its configured backup
                    settings. The run uses disk and network I/O until it
                    finishes.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction onClick={() => trigger.mutate()}>
                    Start backup
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
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
            Listed from the first configured restic repository. Restore copies a
            snapshot into an empty directory; it never overwrites live data.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {restoreStatus && restoreStatus.status !== "idle" && (
            <div
              role="status"
              className="flex flex-wrap items-center gap-2 text-sm"
            >
              {restoreBadge(restoreStatus.status)}
              <span className="font-mono text-xs">
                {restoreStatus.snapshot_id?.slice(0, 8)} →{" "}
                {restoreStatus.destination}
              </span>
              {restoreStatus.finished_at && (
                <span className="text-xs text-muted-foreground">
                  {new Date(restoreStatus.finished_at).toLocaleString()}
                </span>
              )}
              {restoreStatus.error && (
                <span className="w-full text-xs text-destructive">
                  {restoreStatus.error}
                </span>
              )}
            </div>
          )}
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
                  <TableHead />
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
                    <TableCell className="text-right">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={restoring}
                        onClick={() => setRestoreTarget(s)}
                        aria-label={`Restore snapshot ${s.id.slice(0, 8)}`}
                      >
                        <HardDriveUpload className="mr-2 h-4 w-4" />
                        Restore
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <RestoreDialog
        snapshot={restoreTarget}
        onClose={() => setRestoreTarget(null)}
        onStarted={() =>
          queryClient.invalidateQueries({ queryKey: ["backup", "status"] })
        }
      />
    </div>
  );
}
