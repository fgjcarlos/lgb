import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Trash2, UserPlus } from "lucide-react";
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { useUsers, type UserRow } from "@/hooks/useApi";
import { useAuth } from "@/contexts/auth";
import { apiFetch, ApiError } from "@/lib/api";

const ROLES = ["admin", "operator", "viewer"] as const;
type Role = (typeof ROLES)[number];

const PAGE_SIZE = 100;

// Mirrors the server's auth.ValidatePassword (internal/auth/password.go).
const passwordSchema = z
  .string()
  .min(12, "Password must be at least 12 characters")
  .refine(
    (s) => new TextEncoder().encode(s).length <= 72,
    "Password must be at most 72 bytes",
  );

const createUserSchema = z.object({
  username: z.string().min(1, "Username is required"),
  password: passwordSchema,
  role: z.enum(ROLES),
});

type CreateUserValues = z.infer<typeof createUserSchema>;

export function Users() {
  const queryClient = useQueryClient();
  const [offset, setOffset] = useState(0);
  const usersQuery = useUsers({ limit: PAGE_SIZE, offset });
  const count = usersQuery.data?.pagination.count ?? 0;

  const unavailable =
    usersQuery.error instanceof ApiError &&
    (usersQuery.error.status === 404 || usersQuery.error.status === 503);

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["users"] });

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Users</h1>
        <p className="text-sm text-muted-foreground">
          Manage gateway operators. Admin only.
        </p>
      </header>

      {unavailable && (
        <UnavailableBanner message="User management API is not exposed by this gateway build." />
      )}

      <CreateUserCard onCreated={invalidate} />

      <Card>
        <CardHeader>
          <CardTitle>Existing users</CardTitle>
          <CardDescription>
            Role changes ask for confirmation. Deleting an admin is rejected if
            it would leave no admins.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {usersQuery.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : usersQuery.isError && !unavailable ? (
            <p className="text-sm text-destructive">
              {usersQuery.error.message}
            </p>
          ) : !usersQuery.data || usersQuery.data.data.length === 0 ? (
            <p className="text-sm text-muted-foreground">No users yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Username</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Created</TableHead>
                  <TableHead className="w-[120px]">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {usersQuery.data.data.map((user) => (
                  <UserRowItem
                    key={user.id}
                    user={user}
                    onChanged={invalidate}
                  />
                ))}
              </TableBody>
            </Table>
          )}
          {count > PAGE_SIZE && (
            <div className="mt-4 flex items-center justify-between">
              <Button
                variant="outline"
                size="sm"
                disabled={offset === 0}
                onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
              >
                Previous
              </Button>
              <span className="text-sm text-muted-foreground">
                {`Showing ${Math.min(offset + 1, count)}–${Math.min(offset + PAGE_SIZE, count)} of ${count}`}
              </span>
              <Button
                variant="outline"
                size="sm"
                disabled={offset + PAGE_SIZE >= count}
                onClick={() => setOffset((o) => o + PAGE_SIZE)}
              >
                Next
              </Button>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

interface CreateUserCardProps {
  onCreated: () => void;
}

function CreateUserCard({ onCreated }: CreateUserCardProps) {
  const [submitError, setSubmitError] = useState<string | null>(null);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CreateUserValues>({
    resolver: zodResolver(createUserSchema),
    defaultValues: { username: "", password: "", role: "viewer" },
  });

  const create = useMutation({
    mutationFn: (values: CreateUserValues) =>
      apiFetch<{ data: UserRow }>("/api/users", {
        method: "POST",
        
        body: JSON.stringify(values),
      }),
    onSuccess: () => {
      setSubmitError(null);
      reset({ username: "", password: "", role: "viewer" });
      onCreated();
    },
    onError: (err) => {
      const message = err instanceof Error ? err.message : "create failed";
      setSubmitError(message);
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create user</CardTitle>
        <CardDescription>
          Issues a new account with the selected role.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="grid gap-4 md:grid-cols-[1fr_1fr_180px_auto]"
          onSubmit={handleSubmit((values) => create.mutate(values))}
        >
          <div className="space-y-2">
            <Label htmlFor="username">Username</Label>
            <Input id="username" autoComplete="off" {...register("username")} />
            {errors.username && (
              <p className="text-xs text-destructive">
                {errors.username.message}
              </p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              type="password"
              autoComplete="new-password"
              {...register("password")}
            />
            {errors.password && (
              <p className="text-xs text-destructive">
                {errors.password.message}
              </p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="role">Role</Label>
            <select
              id="role"
              className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              {...register("role")}
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>
          <div className="self-end">
            <Button type="submit" disabled={create.isPending}>
              <UserPlus className="mr-2 h-4 w-4" />
              {create.isPending ? "Creating…" : "Create"}
            </Button>
          </div>
          {submitError && (
            <p
              role="alert"
              className="text-sm text-destructive md:col-span-4"
            >
              {submitError}
            </p>
          )}
        </form>
      </CardContent>
    </Card>
  );
}

interface UserRowItemProps {
  user: UserRow;
  onChanged: () => void;
}

function UserRowItem({ user, onChanged }: UserRowItemProps) {
  const {  user: currentUser } = useAuth();
  const [role, setRole] = useState<Role>(user.role);
  // A role picked in the select waits here until the admin confirms it.
  const [pendingRole, setPendingRole] = useState<Role | null>(null);
  const [rowError, setRowError] = useState<string | null>(null);
  const [rowNotice, setRowNotice] = useState<string | null>(null);

  const updateRole = useMutation({
    mutationFn: (newRole: Role) =>
      apiFetch<{ data: UserRow }>(`/api/users/${user.id}/role`, {
        method: "PUT",
        
        body: JSON.stringify({ role: newRole }),
      }),
    onSuccess: () => {
      setRowError(null);
      onChanged();
    },
    onError: (err) => {
      setRowError(err instanceof Error ? err.message : "update failed");
      setRole(user.role);
    },
  });

  const deleteUser = useMutation({
    mutationFn: () =>
      apiFetch<void>(`/api/users/${user.id}`, {
        method: "DELETE",
        
      }),
    onSuccess: () => {
      setRowError(null);
      onChanged();
    },
    onError: (err) => {
      setRowError(err instanceof Error ? err.message : "delete failed");
    },
  });

  const isSelf = currentUser?.id === user.id;

  return (
    <TableRow>
      <TableCell className="font-medium">
        {user.username}
        {isSelf && (
          <span className="ml-2 text-xs text-muted-foreground">(you)</span>
        )}
      </TableCell>
      <TableCell>
        <select
          value={role}
          onChange={(e) => setPendingRole(e.target.value as Role)}
          disabled={updateRole.isPending}
          aria-label={`Role for ${user.username}`}
          className="flex h-9 rounded-md border border-input bg-background px-2 text-sm"
        >
          {ROLES.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
        {rowError && (
          <p className="mt-1 text-xs text-destructive">{rowError}</p>
        )}
        {rowNotice && (
          <p className="mt-1 text-xs text-muted-foreground">{rowNotice}</p>
        )}
        <AlertDialog
          open={pendingRole !== null}
          onOpenChange={(open) => !open && setPendingRole(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                Change {user.username}&apos;s role to {pendingRole}?
              </AlertDialogTitle>
              <AlertDialogDescription>
                {user.username} is currently {role}. The new role applies to
                their next request.
                {isSelf && " You are changing your own role."}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => {
                  if (pendingRole === null) return;
                  setRole(pendingRole);
                  setRowNotice(null);
                  updateRole.mutate(pendingRole);
                }}
              >
                Change role
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {new Date(user.created_at).toLocaleString()}
      </TableCell>
      <TableCell className="flex items-center">
        <ChangePasswordDialog
          user={user}
          onChanged={() => {
            setRowError(null);
            setRowNotice("Password updated.");
          }}
        />
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              aria-label={`Delete ${user.username}`}
              disabled={deleteUser.isPending}
            >
              <Trash2 className="h-4 w-4 text-destructive" />
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Delete {user.username}?</AlertDialogTitle>
              <AlertDialogDescription>
                This action cannot be undone. The user will lose access
                immediately.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => deleteUser.mutate()}
                className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              >
                Delete
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </TableCell>
    </TableRow>
  );
}

interface ChangePasswordDialogProps {
  user: UserRow;
  onChanged: () => void;
}

function ChangePasswordDialog({ user, onChanged }: ChangePasswordDialogProps) {
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);

  const change = useMutation({
    mutationFn: (newPassword: string) =>
      apiFetch<void>(`/api/users/${user.id}/password`, {
        method: "PUT",
        body: JSON.stringify({ password: newPassword }),
      }),
    onSuccess: () => {
      setOpen(false);
      onChanged();
    },
    onError: (err) => {
      setError(err instanceof Error ? err.message : "password change failed");
    },
  });

  function onOpenChange(next: boolean) {
    setOpen(next);
    if (next) {
      setPassword("");
      setConfirm("");
      setError(null);
    }
  }

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    const parsed = passwordSchema.safeParse(password);
    if (!parsed.success) {
      setError(parsed.error.issues[0].message);
      return;
    }
    if (password !== confirm) {
      setError("Passwords do not match");
      return;
    }
    setError(null);
    change.mutate(password);
  }

  const passwordId = `new-password-${user.id}`;
  const confirmId = `confirm-password-${user.id}`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={`Change password for ${user.username}`}
        >
          <KeyRound className="h-4 w-4" />
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={onSubmit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Change password for {user.username}</DialogTitle>
            <DialogDescription>
              At least 12 characters. The user signs in with the new password
              from now on.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor={passwordId}>New password</Label>
            <Input
              id={passwordId}
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor={confirmId}>Confirm password</Label>
            <Input
              id={confirmId}
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="submit" disabled={change.isPending}>
              {change.isPending ? "Updating…" : "Update password"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
