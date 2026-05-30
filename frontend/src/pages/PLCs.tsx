import { useEffect, useState } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Pencil, Plus, Trash2 } from "lucide-react";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
import { UnavailableBanner } from "@/components/UnavailableBanner";
import {
  usePLCs,
  useCreatePLC,
  useUpdatePLC,
  useDeletePLC,
  type PLCRow,
} from "@/hooks/useApi";
import { ApiError } from "@/lib/api";

// Sparkplug B scalar types accepted by the backend (config.validSparkplugType).
const TAG_TYPES = [
  "Boolean",
  "Int8",
  "Int16",
  "Int32",
  "Int64",
  "UInt8",
  "UInt16",
  "UInt32",
  "UInt64",
  "Float",
  "Double",
  "String",
] as const;

// Go duration as parsed by time.ParseDuration — empty is allowed (optional).
const goDuration = z
  .string()
  .refine((v) => v === "" || /^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)+$/.test(v), {
    message: "Must be a Go duration (e.g. 500ms, 1s, 2m)",
  });

const plcSchema = z.object({
  name: z.string().min(1, "Name is required"),
  address: z.string().min(1, "Address is required"),
  slot: z.coerce.number().int().min(0, "0–15").max(15, "0–15"),
  socket_timeout: goDuration,
  scan_rate: goDuration,
  keep_alive: z.boolean(),
  path: z.string(),
  tags: z.array(
    z.object({
      name: z.string().min(1, "Tag name required"),
      type: z.enum(TAG_TYPES),
      writable: z.boolean(),
    }),
  ),
});

type PLCFormValues = z.infer<typeof plcSchema>;

const emptyPLC: PLCFormValues = {
  name: "",
  address: "",
  slot: 0,
  socket_timeout: "",
  scan_rate: "",
  keep_alive: false,
  path: "",
  tags: [],
};

function toFormValues(plc: PLCRow): PLCFormValues {
  return {
    name: plc.name,
    address: plc.address,
    slot: plc.slot,
    socket_timeout: plc.socket_timeout ?? "",
    scan_rate: plc.scan_rate ?? "",
    keep_alive: plc.keep_alive,
    path: plc.path ?? "",
    tags: plc.tags.map((t) => ({
      name: t.name,
      type: t.type as (typeof TAG_TYPES)[number],
      writable: t.writable,
    })),
  };
}

export function PLCs() {
  const plcsQuery = usePLCs();
  const [editing, setEditing] = useState<PLCRow | null>(null);
  const [creating, setCreating] = useState(false);

  const unavailable =
    plcsQuery.error instanceof ApiError &&
    (plcsQuery.error.status === 404 || plcsQuery.error.status === 503);

  const dialogOpen = creating || editing !== null;
  const closeDialog = () => {
    setCreating(false);
    setEditing(null);
  };

  return (
    <div className="space-y-6">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">PLCs</h1>
          <p className="text-sm text-muted-foreground">
            Manage PLC connections and their tag maps. Admin only. Changes take
            effect immediately — no restart required.
          </p>
        </div>
        <Button onClick={() => setCreating(true)}>
          <Plus className="mr-2 h-4 w-4" />
          Add PLC
        </Button>
      </header>

      {unavailable && (
        <UnavailableBanner message="PLC management API is not exposed by this gateway build." />
      )}

      <Card>
        <CardHeader>
          <CardTitle>Configured PLCs</CardTitle>
          <CardDescription>
            Each PLC is polled on its scan rate. Deleting one stops its worker.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {plcsQuery.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : plcsQuery.isError && !unavailable ? (
            <p className="text-sm text-destructive">
              {plcsQuery.error.message}
            </p>
          ) : !plcsQuery.data || plcsQuery.data.data.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No PLCs configured yet. Add one to start polling.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Address</TableHead>
                  <TableHead>Scan rate</TableHead>
                  <TableHead>Tags</TableHead>
                  <TableHead className="w-[120px]">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {plcsQuery.data.data.map((plc) => (
                  <PLCRowItem
                    key={plc.name}
                    plc={plc}
                    onEdit={() => setEditing(plc)}
                  />
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={dialogOpen}
        onOpenChange={(open) => {
          if (!open) closeDialog();
        }}
      >
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-[640px]">
          {dialogOpen && (
            <PLCForm
              editing={editing}
              onDone={closeDialog}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

interface PLCRowItemProps {
  plc: PLCRow;
  onEdit: () => void;
}

function PLCRowItem({ plc, onEdit }: PLCRowItemProps) {
  const deletePLC = useDeletePLC();
  const [rowError, setRowError] = useState<string | null>(null);

  return (
    <TableRow>
      <TableCell className="font-medium">{plc.name}</TableCell>
      <TableCell className="font-mono text-xs">{plc.address}</TableCell>
      <TableCell>{plc.scan_rate || "—"}</TableCell>
      <TableCell>{plc.tags.length}</TableCell>
      <TableCell>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="icon"
            aria-label={`Edit ${plc.name}`}
            onClick={onEdit}
          >
            <Pencil className="h-4 w-4" />
          </Button>
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                aria-label={`Delete ${plc.name}`}
                disabled={deletePLC.isPending}
              >
                <Trash2 className="h-4 w-4 text-destructive" />
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Delete {plc.name}?</AlertDialogTitle>
                <AlertDialogDescription>
                  This stops polling immediately and removes the PLC and its
                  tags. This action cannot be undone.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() =>
                    deletePLC.mutate(plc.name, {
                      onError: (err) =>
                        setRowError(
                          err instanceof Error ? err.message : "delete failed",
                        ),
                    })
                  }
                  className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
                >
                  Delete
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
        {rowError && (
          <p className="mt-1 text-xs text-destructive">{rowError}</p>
        )}
      </TableCell>
    </TableRow>
  );
}

interface PLCFormProps {
  editing: PLCRow | null;
  onDone: () => void;
}

function PLCForm({ editing, onDone }: PLCFormProps) {
  const createPLC = useCreatePLC();
  const updatePLC = useUpdatePLC();
  const [submitError, setSubmitError] = useState<string | null>(null);

  const {
    register,
    control,
    handleSubmit,
    formState: { errors },
  } = useForm<PLCFormValues>({
    resolver: zodResolver(plcSchema),
    defaultValues: editing ? toFormValues(editing) : emptyPLC,
  });

  const { fields, append, remove } = useFieldArray({ control, name: "tags" });

  // Keep submit error in sync with the active mutation.
  useEffect(() => setSubmitError(null), [editing]);

  const onSubmit = (values: PLCFormValues) => {
    setSubmitError(null);
    const onError = (err: unknown) =>
      setSubmitError(err instanceof Error ? err.message : "save failed");

    if (editing) {
      updatePLC.mutate(
        { name: editing.name, plc: values },
        { onSuccess: onDone, onError },
      );
    } else {
      createPLC.mutate(values, { onSuccess: onDone, onError });
    }
  };

  const pending = createPLC.isPending || updatePLC.isPending;

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
      <DialogHeader>
        <DialogTitle>{editing ? `Edit ${editing.name}` : "Add PLC"}</DialogTitle>
        <DialogDescription>
          Connection settings and the tags to poll. Durations use Go syntax
          (e.g. <code>500ms</code>, <code>1s</code>).
        </DialogDescription>
      </DialogHeader>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="name">Name</Label>
          <Input
            id="name"
            autoComplete="off"
            disabled={!!editing}
            {...register("name")}
          />
          {errors.name && (
            <p className="text-xs text-destructive">{errors.name.message}</p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="address">Address</Label>
          <Input id="address" autoComplete="off" {...register("address")} />
          {errors.address && (
            <p className="text-xs text-destructive">{errors.address.message}</p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="slot">Slot (0–15)</Label>
          <Input
            id="slot"
            type="number"
            min={0}
            max={15}
            {...register("slot")}
          />
          {errors.slot && (
            <p className="text-xs text-destructive">{errors.slot.message}</p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="scan_rate">Scan rate</Label>
          <Input
            id="scan_rate"
            placeholder="1s"
            autoComplete="off"
            {...register("scan_rate")}
          />
          {errors.scan_rate && (
            <p className="text-xs text-destructive">
              {errors.scan_rate.message}
            </p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="socket_timeout">Socket timeout</Label>
          <Input
            id="socket_timeout"
            placeholder="5s"
            autoComplete="off"
            {...register("socket_timeout")}
          />
          {errors.socket_timeout && (
            <p className="text-xs text-destructive">
              {errors.socket_timeout.message}
            </p>
          )}
        </div>
        <div className="space-y-2">
          <Label htmlFor="path">Path</Label>
          <Input id="path" autoComplete="off" {...register("path")} />
        </div>
      </div>

      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" {...register("keep_alive")} />
        Keep connection alive between scans
      </label>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <Label>Tags</Label>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => append({ name: "", type: "Float", writable: false })}
          >
            <Plus className="mr-2 h-4 w-4" />
            Add tag
          </Button>
        </div>

        {fields.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            No tags yet. A PLC with no tags polls nothing.
          </p>
        ) : (
          <div className="space-y-2">
            {fields.map((field, index) => (
              <div
                key={field.id}
                className="grid grid-cols-[1fr_140px_auto_auto] items-center gap-2"
              >
                <div>
                  <Input
                    placeholder="Motor.Speed"
                    autoComplete="off"
                    {...register(`tags.${index}.name`)}
                  />
                  {errors.tags?.[index]?.name && (
                    <p className="mt-1 text-xs text-destructive">
                      {errors.tags[index]?.name?.message}
                    </p>
                  )}
                </div>
                <select
                  className="flex h-10 rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  {...register(`tags.${index}.type`)}
                >
                  {TAG_TYPES.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>
                <label className="flex items-center gap-1 text-xs text-muted-foreground">
                  <input
                    type="checkbox"
                    {...register(`tags.${index}.writable`)}
                  />
                  Writable
                </label>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={`Remove tag ${index + 1}`}
                  onClick={() => remove(index)}
                >
                  <Trash2 className="h-4 w-4 text-destructive" />
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>

      {submitError && (
        <p role="alert" className="text-sm text-destructive">
          {submitError}
        </p>
      )}

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending ? "Saving…" : editing ? "Save changes" : "Create PLC"}
        </Button>
      </DialogFooter>
    </form>
  );
}
