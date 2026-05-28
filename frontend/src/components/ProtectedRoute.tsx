import { Navigate, useLocation } from "react-router-dom";
import type { ReactNode } from "react";
import { useAuth } from "@/contexts/auth";
import type { AuthUser } from "@/lib/jwt";

interface ProtectedRouteProps {
  children: ReactNode;
  requiredRole?: AuthUser["role"];
}

const ROLE_RANK: Record<AuthUser["role"], number> = {
  viewer: 1,
  operator: 2,
  admin: 3,
};

function hasRole(user: AuthUser, required: AuthUser["role"]): boolean {
  return ROLE_RANK[user.role] >= ROLE_RANK[required];
}

export function ProtectedRoute({ children, requiredRole }: ProtectedRouteProps) {
  const { isAuthenticated, user } = useAuth();
  const location = useLocation();

  if (!isAuthenticated || !user) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (requiredRole && !hasRole(user, requiredRole)) {
    return <Navigate to="/" replace />;
  }

  return <>{children}</>;
}
