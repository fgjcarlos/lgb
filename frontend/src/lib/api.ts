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

interface ApiFetchOptions extends Omit<RequestInit, "body"> {
  body?: BodyInit | null;
}

export async function apiFetch<T>(
  path: string,
  options: ApiFetchOptions = {},
): Promise<T> {
  const { headers, body, ...rest } = options;
  const mergedHeaders = new Headers(headers);
  if (!mergedHeaders.has("Content-Type") && body) {
    mergedHeaders.set("Content-Type", "application/json");
  }
  // The browser sends the HttpOnly session cookie on same-origin XHR.
  // We make the intent explicit here so future maintainers do not add
  // a header-based fallback that would re-introduce the localStorage
  // token we are explicitly avoiding (Fix for #78).
  const resp = await fetch(path, {
    ...rest,
    body,
    credentials: "same-origin",
    headers: mergedHeaders,
  });

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