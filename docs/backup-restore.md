# Backup restore runbook

LGB backs up with [restic](https://restic.net) (see [ADR-0008](adr/0008-backups.md)). This runbook covers
restoring a snapshot on the same host and on a different host.

## What a snapshot contains

Before each backup, LGB writes a consistent copy of the historian database (`VACUUM INTO`) to
`<dataDir>/backup-tmp/lgb-snapshot.db` and backs up that file. The live historian is `<dataDir>/lgb.db`.

This is a **historian-only** backup: it contains the consistent SQLite historian snapshot, not the rest of the gateway state. User accounts (`users.db`), PLC configuration/store, YAML configuration, TLS material, and the `restic` repository password are **not** included. Back up those files and secrets separately using your approved protected backup process; do not assume restoring the historian restores users or configuration. Keep the repository URL and password separately: without them, no snapshot can be read.

## Safety model: restore to a path, then swap manually

A restore never writes into the live data directory. It goes into a new or empty directory, and you swap
files while the gateway is stopped. This keeps a running gateway from reading a half-restored database,
and you can inspect the result before it goes into service.

## Backup checks and manual commands

Set `historian.retentionDays` above zero and configure `backup.repos` with a repository URL and password stored in protected configuration. Set `backup.interval` for scheduled backups. The Compose example does not configure a backup destination; mount a protected, site-specific copy of its LGB configuration and provision any storage/credentials required by your backend before relying on backups.

Use the **Backup** UI to trigger a snapshot on a running gateway. Use `lgb backup run --config /path/to/lgb.yaml` from an instance with access to the configured data directory and restic; avoid overlapping it with scheduled or UI runs. Confirm the command succeeds and the repository contains a fresh snapshot. Run `lgb backup check --config /path/to/lgb.yaml` to check all configured repositories. For a single repository, use restic directly with the password supplied securely:

```bash
export RESTIC_PASSWORD='<repository password>'
restic -r '<repository URL>' snapshots
restic -r '<repository URL>' check
```

`check` verifies repository integrity; it does not prove the gateway can start from a restored database. Perform a test restore periodically and keep config, user/PLC state, TLS files, and repository credentials in a separate protected backup.

## Restore from the UI

1. Sign in as an **admin** and open **Backup**.
2. In **Snapshots**, click **Restore** on the snapshot you want. The list comes from the first repository
   in `backup.repos`.
3. Enter an absolute destination path. The directory must be missing or empty, and the gateway process
   must be able to write to it, for example `/var/lib/lgb-restore/2026-11-23`.
4. Type the snapshot ID to confirm, then click **Restore**.
5. The Snapshots card shows **Restoring** until restic finishes, then **Restore succeeded** or
   **Restore failed** with the restic error.

Each accepted restore is written to the audit log as `backup.restore`, with the admin user, the client IP
and `repo=<index> snapshot=<id> destination=<path>`.

The API equivalent is `POST /api/backup/restore` with `{"repo": 0, "snapshot_id": "<id>", "destination":
"<path>"}`. `repo` is the 0-based index into `backup.repos`. Progress is reported in the `restore` field of
`GET /api/backup/status`. See [openapi.yaml](api/openapi.yaml).

restic keeps the original absolute path under the destination. A snapshot taken with
`gateway.dataDir: /data` restores to `<destination>/data/backup-tmp/lgb-snapshot.db`.

## Put the restored database into service

1. Stop the gateway: `systemctl stop lgb` or `docker stop <container>`.
2. Keep the current database:
   `mv <dataDir>/lgb.db <dataDir>/lgb.db.pre-restore`. Move `lgb.db-wal` and `lgb.db-shm` too if they
   exist. A stale WAL must not be replayed onto the restored file.
3. Copy the restored file into place:
   `cp <destination>/<dataDir>/backup-tmp/lgb-snapshot.db <dataDir>/lgb.db`.
4. Give the file to the service user and set mode `0600`. For Compose, use the host `LGB_DATA_DIR` path and ownership `65532:65532` (or the daemon's mapped IDs); for a native install use the account running LGB.
5. Start the gateway and check `lgb doctor`, the historian view, and application logs. Keep the `.pre-restore` database and restore directory until recovery has been verified; they are your rollback copy. If validation fails, stop LGB, move the failed database aside, restore the saved `.pre-restore` database and any matching WAL/SHM files, then restart and investigate. Do not delete rollback copies until the recovered service is accepted.

## Restore on another host

Use this to recover after losing a host. The steps assume the old gateway is unreachable.

1. Install LGB on the new host ([deployment](deployment.md)), including the `restic` binary. The Docker
   image ships it.
2. Restore your saved YAML config and secrets. `backup.repos` must point at the same repository URL with
   the same password. Do not start the gateway yet if the old host may still be running against the same
   repository with the same PLCs.
3. Choose one of the following:
   - **UI:** start the gateway with an empty `dataDir`, sign in as admin and restore into a directory
     outside `dataDir`, as above. Then stop the gateway and follow
     [Put the restored database into service](#put-the-restored-database-into-service).
   - **CLI:** no running gateway is needed:

     ```bash
     export RESTIC_PASSWORD='<repository password>'
     restic -r <repository URL> snapshots
     restic -r <repository URL> restore <snapshot-id> --target /var/lib/lgb-restore
     ```

     Then copy `/var/lib/lgb-restore/<old dataDir>/backup-tmp/lgb-snapshot.db` to `<dataDir>/lgb.db`,
     and fix its owner and mode.
4. Start the gateway and check `lgb doctor`, the PLC status page and the historian.
5. Keep the scheduled backups pointed at the same repository. `restic snapshots` lists the new host's
   hostname from its first backup on.
