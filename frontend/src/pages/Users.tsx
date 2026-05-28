import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2, UserPlus } from "lucide-react";
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
import { UnavailableBanner } from "@/components/UnavailableBanner";
import { useUsers, type UserRow } from "@/hooks/useApi";
import { useAuth } from "@/contexts/auth";
import { apiFetch, ApiError } from "@/lib/api";

const ROLES = ["admin", "operator", "viewer"] as const;
type Role = (typeof ROLES)[number];

const createUserSchema = z.object({
  username: z.string().min(1, "Username is required"),
  password: z.string().min(6, "Password must be at least 6 characters"),
  role: z.enum(ROLES),
});

type CreateUserValues = z.infer<typeof createUserSchema>;

export function Users() {
  const queryClient = useQueryClient();
  const usersQuery = useUsers();

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
            Roles can be edited inline. Deleting an admin is rejected if it
            would leave no admins.
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
        </CardContent>
      </Card>
    </div>
  );
}

interface CreateUserCardProps {
  onCreated: () => void;
}

function CreateUserCard({ onCreated }: CreateUserCardProps) {
  const { token } = useAuth();
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
        token,
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
  const { token, user: currentUser } = useAuth();
  const [role, setRole] = useState<Role>(user.role);
  const [rowError, setRowError] = useState<string | null>(null);

  const updateRole = useMutation({
    mutationFn: (newRole: Role) =>
      apiFetch<{ data: UserRow }>(`/api/users/${user.id}/role`, {
        method: "PUT",
        token,
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
        token,
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
          onChange={(e) => {
            const next = e.target.value as Role;
            setRole(next);
            updateRole.mutate(next);
          }}
          disabled={updateRole.isPending}
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
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {new Date(user.created_at).toLocaleString()}
      </TableCell>
      <TableCell>
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
