export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type LogoutFn = () => void;
let onUnauthorized: LogoutFn | null = null;

export function setAuthLogout(fn: LogoutFn | null) {
  onUnauthorized = fn;
}

interface ApiFetchOptions extends RequestInit {
  token?: string | null;
}

export async function apiFetch<T>(
  path: string,
  options: ApiFetchOptions = {},
): Promise<T> {
  const { token, headers, ...rest } = options;
  const mergedHeaders = new Headers(headers);
  if (!mergedHeaders.has("Content-Type") && rest.body) {
    mergedHeaders.set("Content-Type", "application/json");
  }
  if (token) {
    mergedHeaders.set("Authorization", `Bearer ${token}`);
  }

  const resp = await fetch(path, { ...rest, headers: mergedHeaders });

  if (resp.status === 401) {
    onUnauthorized?.();
    throw new ApiError(401, "unauthorized", "session expired");
  }

  if (!resp.ok) {
    let code = "request_failed";
    let message = `request failed with status ${resp.status}`;
    try {
      const body = (await resp.json()) as { error?: { code: string; message: string } };
      if (body.error) {
        code = body.error.code;
        message = body.error.message;
      }
    } catch {
      // swallow parse errors; fall back to defaults
    }
    throw new ApiError(resp.status, code, message);
  }

  if (resp.status === 204) {
    return undefined as T;
  }
  return (await resp.json()) as T;
}
