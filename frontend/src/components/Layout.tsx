import { NavLink, Outlet } from "react-router-dom";
import { LogOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { useAuth } from "@/contexts/auth";
import { cn } from "@/lib/utils";
import { routes, navIcon as BrandIcon } from "@/router";
import type { AuthUser } from "@/lib/jwt";

const ROLE_RANK: Record<AuthUser["role"], number> = {
  viewer: 1,
  operator: 2,
  admin: 3,
};

export function Layout() {
  const { user, logout } = useAuth();

  const visibleRoutes = routes.filter((r) => {
    if (!r.requiredRole) return true;
    if (!user) return false;
    return ROLE_RANK[user.role] >= ROLE_RANK[r.requiredRole];
  });

  return (
    <div className="flex h-screen w-screen bg-background text-foreground">
      <aside className="flex w-64 flex-col border-r bg-muted/30">
        <div className="flex h-16 items-center gap-2 border-b px-4">
          <BrandIcon className="h-5 w-5 text-primary" />
          <span className="font-semibold tracking-tight">LGB Gateway</span>
        </div>
        <nav className="flex-1 space-y-1 p-2">
          {visibleRoutes.map((route) => {
            const Icon = route.icon;
            return (
              <NavLink
                key={route.path}
                to={route.path}
                end={route.path === "/"}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                    isActive
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                  )
                }
              >
                <Icon className="h-4 w-4" />
                <span>{route.label}</span>
              </NavLink>
            );
          })}
        </nav>
        <Separator />
        <div className="p-3">
          <div className="mb-2 text-xs text-muted-foreground">
            Signed in as{" "}
            <span className="font-medium text-foreground">
              {user?.username ?? "?"}
            </span>{" "}
            ({user?.role ?? "?"})
          </div>
          <Button variant="outline" size="sm" className="w-full" onClick={logout}>
            <LogOut className="mr-2 h-4 w-4" />
            Sign out
          </Button>
        </div>
      </aside>
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}
