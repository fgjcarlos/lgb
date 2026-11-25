import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProtectedRoute } from "@/components/ProtectedRoute";
import type { AuthUser } from "@/lib/jwt";

const auth = vi.hoisted(() => ({ user: null as AuthUser | null }));

vi.mock("@/contexts/auth", () => ({
  useAuth: () => ({ user: auth.user, isAuthenticated: auth.user !== null }),
}));

function LoginProbe() {
  const location = useLocation();
  return <div>login from {(location.state as { from?: string } | null)?.from}</div>;
}

function renderAt(path: string, requiredRole?: AuthUser["role"]) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/login" element={<LoginProbe />} />
        <Route path="/" element={<div>home</div>} />
        <Route
          path="/admin"
          element={
            <ProtectedRoute requiredRole={requiredRole}>
              <div>secret</div>
            </ProtectedRoute>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

const as = (role: AuthUser["role"]): AuthUser => ({ id: 1, username: "u", role });

beforeEach(() => {
  auth.user = null;
});

describe("ProtectedRoute", () => {
  it("redirects anonymous users to /login, remembering the path", () => {
    renderAt("/admin");
    expect(screen.getByText("login from /admin")).toBeInTheDocument();
    expect(screen.queryByText("secret")).not.toBeInTheDocument();
  });

  it("redirects users below the required role to /", () => {
    auth.user = as("viewer");
    renderAt("/admin", "operator");
    expect(screen.getByText("home")).toBeInTheDocument();
    expect(screen.queryByText("secret")).not.toBeInTheDocument();
  });

  it.each([
    ["operator", "operator"],
    ["admin", "operator"],
    ["admin", "admin"],
  ] as const)("renders for %s when %s is required", (role, required) => {
    auth.user = as(role);
    renderAt("/admin", required);
    expect(screen.getByText("secret")).toBeInTheDocument();
  });

  it("operator is redirected from admin-only routes", () => {
    auth.user = as("operator");
    renderAt("/admin", "admin");
    expect(screen.getByText("home")).toBeInTheDocument();
  });

  it("renders for any signed-in user when no role is required", () => {
    auth.user = as("viewer");
    renderAt("/admin");
    expect(screen.getByText("secret")).toBeInTheDocument();
  });
});
