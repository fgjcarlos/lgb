import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import {
  decodeJwtPayload,
  isTokenExpired,
  userFromPayload,
  type AuthUser,
} from "@/lib/jwt";
import { apiFetch, setAuthLogout } from "@/lib/api";

const TOKEN_KEY = "lgb_token";

interface TokenResponse {
  token: string;
  expires_at: string;
}

interface AuthContextValue {
  token: string | null;
  user: AuthUser | null;
  isAuthenticated: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function readStoredToken(): { token: string; user: AuthUser } | null {
  const stored = localStorage.getItem(TOKEN_KEY);
  if (!stored) return null;
  try {
    const payload = decodeJwtPayload(stored);
    if (isTokenExpired(payload)) {
      localStorage.removeItem(TOKEN_KEY);
      return null;
    }
    return { token: stored, user: userFromPayload(payload) };
  } catch {
    localStorage.removeItem(TOKEN_KEY);
    return null;
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<{ token: string | null; user: AuthUser | null }>(
    () => readStoredToken() ?? { token: null, user: null },
  );

  const logout = useCallback(() => {
    localStorage.removeItem(TOKEN_KEY);
    setState({ token: null, user: null });
  }, []);

  useEffect(() => {
    setAuthLogout(logout);
    return () => setAuthLogout(null);
  }, [logout]);

  const login = useCallback(async (username: string, password: string) => {
    const resp = await apiFetch<TokenResponse>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    const payload = decodeJwtPayload(resp.token);
    localStorage.setItem(TOKEN_KEY, resp.token);
    setState({ token: resp.token, user: userFromPayload(payload) });
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      token: state.token,
      user: state.user,
      isAuthenticated: state.token !== null && state.user !== null,
      login,
      logout,
    }),
    [state, login, logout],
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
