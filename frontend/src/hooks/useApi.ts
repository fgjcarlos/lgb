import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { apiFetch, ApiError } from "@/lib/api";
import { useAuth } from "@/contexts/auth";

export interface PaginationEnvelope {
  limit: number;
  offset: number;
  count: number;
}

export interface PagedResponse<T> {
  data: T[];
  pagination: PaginationEnvelope;
}

export interface TagRow {
  plc: string;
  tag: string;
  value: number | string | boolean | null;
  quality: string;
  timestamp: string;
}

export function useCurrentTags(
  params: { limit?: number; offset?: number } = {},
): UseQueryResult<PagedResponse<TagRow>, ApiError | Error> {
  const { token } = useAuth();
  const limit = params.limit ?? 100;
  const offset = params.offset ?? 0;
  return useQuery({
    queryKey: ["tags", "current", limit, offset],
    queryFn: () =>
      apiFetch<PagedResponse<TagRow>>(
        `/api/tags/current?limit=${limit}&offset=${offset}`,
        { token },
      ),
    enabled: !!token,
    refetchInterval: 10_000,
  });
}

export interface MappingTag {
  name: string;
  type: string;
}

export interface Mapping {
  plc: string;
  address: string;
  scan_rate: string;
  tags: MappingTag[];
}

export function useMappings(): UseQueryResult<
  { data: Mapping[] },
  ApiError | Error
> {
  const { token } = useAuth();
  return useQuery({
    queryKey: ["config", "mappings"],
    queryFn: () =>
      apiFetch<{ data: Mapping[] }>("/api/config/mappings", { token }),
    enabled: !!token,
  });
}

// ─── Stub hooks: filled in by PRs 4 and 5 ──────────────────────────────────

export interface HistorianSample {
  plc_name: string;
  tag: string;
  timestamp: string;
  value: number | string | boolean | null;
  quality: string;
}

export interface HistorianQueryParams {
  plc?: string;
  tag: string;
  from?: string;
  to?: string;
  limit?: number;
}

export function useHistorianQuery(
  params: HistorianQueryParams | null,
): UseQueryResult<PagedResponse<HistorianSample>, ApiError | Error> {
  const { token } = useAuth();
  return useQuery({
    queryKey: ["historian", params],
    queryFn: () => {
      if (!params) throw new Error("no query params");
      const search = new URLSearchParams();
      if (params.plc) search.set("plc", params.plc);
      search.set("tag", params.tag);
      if (params.from) search.set("from", params.from);
      if (params.to) search.set("to", params.to);
      if (params.limit) search.set("limit", String(params.limit));
      return apiFetch<PagedResponse<HistorianSample>>(
        `/api/historian/query?${search.toString()}`,
        { token },
      );
    },
    enabled: !!token && !!params,
  });
}

export interface UserRow {
  id: number;
  username: string;
  role: "admin" | "operator" | "viewer";
  created_at: string;
}

export function useUsers(): UseQueryResult<
  PagedResponse<UserRow>,
  ApiError | Error
> {
  const { token } = useAuth();
  return useQuery({
    queryKey: ["users"],
    queryFn: () => apiFetch<PagedResponse<UserRow>>("/api/users", { token }),
    enabled: !!token,
  });
}

export interface BackupStatusResponse {
  status: "idle" | "running" | "failed";
  last_run?: string | null;
  last_error?: string;
}

export function useBackupStatus(
  enabled = true,
): UseQueryResult<BackupStatusResponse, ApiError | Error> {
  const { token } = useAuth();
  return useQuery({
    queryKey: ["backup", "status"],
    queryFn: () =>
      apiFetch<BackupStatusResponse>("/api/backup/status", { token }),
    enabled: !!token && enabled,
  });
}

export interface DoctorCheck {
  name: string;
  status: "pass" | "warn" | "fail";
  message: string;
}

export interface DoctorResponse {
  checks: DoctorCheck[];
  overall: "pass" | "warn" | "fail";
}

export function useDoctorChecks(): UseQueryResult<
  DoctorResponse,
  ApiError | Error
> {
  const { token } = useAuth();
  return useQuery({
    queryKey: ["doctor"],
    queryFn: () => apiFetch<DoctorResponse>("/api/doctor", { token }),
    enabled: !!token,
  });
}

// ─── PLC config CRUD (admin-managed; backed by the SQLite plc store) ───────

export interface PLCTag {
  name: string;
  type: string;
  writable: boolean;
}

export interface PLCRow {
  name: string;
  address: string;
  slot: number;
  socket_timeout: string;
  scan_rate: string;
  keep_alive: boolean;
  path: string;
  tags: PLCTag[];
}

export function usePLCs(): UseQueryResult<
  { data: PLCRow[] },
  ApiError | Error
> {
  const { token } = useAuth();
  return useQuery({
    queryKey: ["plcs"],
    queryFn: () => apiFetch<{ data: PLCRow[] }>("/api/plcs", { token }),
    enabled: !!token,
  });
}

export function usePLC(
  name: string | null,
): UseQueryResult<{ data: PLCRow }, ApiError | Error> {
  const { token } = useAuth();
  return useQuery({
    queryKey: ["plcs", name],
    queryFn: () =>
      apiFetch<{ data: PLCRow }>(`/api/plcs/${encodeURIComponent(name!)}`, {
        token,
      }),
    enabled: !!token && !!name,
  });
}

export function useCreatePLC(): UseMutationResult<
  { data: PLCRow },
  ApiError | Error,
  PLCRow
> {
  const { token } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (plc: PLCRow) =>
      apiFetch<{ data: PLCRow }>("/api/plcs", {
        method: "POST",
        token,
        body: JSON.stringify(plc),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["plcs"] }),
  });
}

export function useUpdatePLC(): UseMutationResult<
  { data: PLCRow },
  ApiError | Error,
  { name: string; plc: PLCRow }
> {
  const { token } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ name, plc }: { name: string; plc: PLCRow }) =>
      apiFetch<{ data: PLCRow }>(`/api/plcs/${encodeURIComponent(name)}`, {
        method: "PUT",
        token,
        body: JSON.stringify(plc),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["plcs"] }),
  });
}

export function useDeletePLC(): UseMutationResult<
  void,
  ApiError | Error,
  string
> {
  const { token } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) =>
      apiFetch<void>(`/api/plcs/${encodeURIComponent(name)}`, {
        method: "DELETE",
        token,
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["plcs"] }),
  });
}
