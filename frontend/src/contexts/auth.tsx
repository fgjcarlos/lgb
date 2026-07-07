import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  decodeJwtPayload,
  isTokenExpired,
  sessionUserSchema,
  userFromPayload,
  type AuthUser,
  type JwtPayload,
} from "@/lib/jwt";
import { apiFetch, setAuthLogout } from "@/lib/api";

interface AuthContextValue {
  user: AuthUser | null;
  isAuthenticated: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  /**
   * Returns the current JWT held in memory. Available only to the WS
   * bootstrap so the WebSocket can re-authenticate after the upgrade;
   * the value is never persisted to localStorage / sessionStorage and is
   * lost on page reload (the browser session cookie still keeps the
   * user signed in across reloads via /api/auth/me). Fix for #78.
   */
  getWsToken: () => string | null;
}

const AuthContext = createContext<AuthContextValue | null>(null);

// REFRESH_LEAD_SECONDS — how early before token expiry to refresh.
// 60s gives the refresh call enough time to complete on a flaky
// network before the current token becomes unusable, while staying
// short enough that a user actively using the UI never sees the
// "session expired" toast. Fix for #78.
const REFRESH_LEAD_SECONDS = 60;

interface AuthState {
  user: AuthUser | null;
  token: string | null; // in-memory only, never persisted
  expiresAt: number | null; // unix seconds, derived from JWT.exp
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    user: null,
    token: null,
    expiresAt: null,
  });
  const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearRefreshTimer = useCallback(() => {
    if (refreshTimerRef.current !== null) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
  }, []);

  const scheduleRefresh = useCallback(
    (expiresAt: number | null) => {
      clearRefreshTimer();
      if (expiresAt === null) return;
      const nowSec = Date.now() / 1000;
      const delayMs = Math.max(5_000, (expiresAt - REFRESH_LEAD_SECONDS - nowSec) * 1000);
      refreshTimerRef.current = setTimeout(() => {
        void doRefresh();
      }, delayMs);
    },
    // doRefresh is defined below and stable via useCallback; the eslint
    // rule complains because of the lexical order, so we ignore the
    // missing dep here — it's intentional.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [clearRefreshTimer],
  );

  const doRefresh = useCallback(async () => {
    try {
      const resp = await apiFetch<{ token: string; expires_at: string }>(
        "/api/auth/refresh",
        { method: "POST" },
      );
      const claims = decodeJwtPayload(resp.token);
      setState((prev) => ({
        ...prev,
        token: resp.token,
        expiresAt: claims.exp,
        user: userFromPayload(claims),
      }));
      scheduleRefresh(claims.exp);
    } catch {
      // 401 etc. — let the global onUnauthorized hook drop us to /login.
      clearRefreshTimer();
    }
  }, [scheduleRefresh, clearRefreshTimer]);

  const logout = useCallback(async () => {
    clearRefreshTimer();
    try {
      await apiFetch<void>("/api/auth/logout", { method: "POST" });
    } catch {
      // ignore — we always clear local state below
    }
    setState({ user: null, token: null, expiresAt: null });
  }, [clearRefreshTimer]);

  // On mount: ask /api/auth/me. If the session cookie is valid, the
  // server returns the user + expires_at; we then issue a fresh JWT via
  // /api/auth/refresh so the WS bootstrap has a token in memory. This
  // works across page reloads because the cookie is HttpOnly and rides
  // along on same-origin XHR automatically.
  useEffect(() => {
    setAuthLogout(logout);
    let cancelled = false;

    (async () => {
      try {
        const me = await apiFetch<unknown>("/api/auth/me");
        const parsed = sessionUserSchema.parse(me); // Zod-validated
        if (cancelled) return;
        setState((prev) => ({
          ...prev,
          user: {
            id: parsed.user.id,
            username: parsed.user.username,
            role: parsed.user.role,
          },
          expiresAt: Math.floor(Date.parse(parsed.expires_at) / 1000),
        }));
        // Pull a fresh token for the WS bootstrap.
        await doRefresh();
      } catch {
        // 401 / invalid / network — user stays logged out.
        if (!cancelled) {
          setState({ user: null, token: null, expiresAt: null });
        }
      }
    })();

    return () => {
      cancelled = true;
      setAuthLogout(null);
      clearRefreshTimer();
    };
  }, [logout, doRefresh, clearRefreshTimer]);

  const login = useCallback(async (username: string, password: string) => {
    // Login response now still carries the token in JSON for backward
    // compat with CLI / API tooling — the SPA discards it after
    // decoding the claims, then asks for a fresh WS token via
    // doRefresh so the in-memory copy matches what /api/auth/me just
    // authorised. The HttpOnly cookie set by the server is the actual
    // session transport; the in-memory token is only for the WS.
    const resp = await apiFetch<{ token: string; expires_at: string }>(
      "/api/auth/login",
      { method: "POST", body: JSON.stringify({ username, password }) },
    );
    const claims = decodeJwtPayload(resp.token);
    setState({
      user: userFromPayload(claims),
      token: resp.token,
      expiresAt: claims.exp,
    });
    scheduleRefresh(claims.exp);
  }, [scheduleRefresh]);

  const getWsToken = useCallback((): string | null => {
    if (!state.token || !state.expiresAt) return null;
    if (isTokenExpired({ ...placeholderClaims(state.expiresAt), exp: state.expiresAt } as JwtPayload)) {
      return null;
    }
    return state.token;
  }, [state.token, state.expiresAt]);

  const value = useMemo<AuthContextValue>(
    () => ({
      user: state.user,
      isAuthenticated: state.user !== null,
      login,
      logout,
      getWsToken,
    }),
    [state.user, login, logout, getWsToken],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return ctx;
}

// placeholderClaims fabricates a JwtPayload skeleton that satisfies
// isTokenExpired's only consumer (the .exp field). Used so
// getWsToken can defer to isTokenExpired without re-validating the
// full claims shape — the token was already validated by decodeJwtPayload
// when it entered the state.
function placeholderClaims(exp: number): JwtPayload {
  return {
    uid: 0,
    sub: "",
    role: "viewer",
    exp,
    iat: 0,
    iss: "",
  };
}