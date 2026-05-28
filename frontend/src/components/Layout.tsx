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
      <aside className="flex w-16 flex-col border-r bg-muted/30 md:w-64">
        <div className="flex h-16 items-center gap-2 border-b px-3 md:px-4">
          <BrandIcon className="h-5 w-5 shrink-0 text-primary" />
          <span className="hidden font-semibold tracking-tight md:inline">
            LGB Gateway
          </span>
        </div>
        <nav className="flex-1 space-y-1 p-2">
          {visibleRoutes.map((route) => {
            const Icon = route.icon;
            return (
              <NavLink
                key={route.path}
                to={route.path}
                end={route.path === "/"}
                title={route.label}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                    "justify-center md:justify-start",
                    isActive
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                  )
                }
              >
                <Icon className="h-4 w-4 shrink-0" />
                <span className="hidden md:inline">{route.label}</span>
              </NavLink>
            );
          })}
        </nav>
        <Separator />
        <div className="p-2 md:p-3">
          <div className="mb-2 hidden text-xs text-muted-foreground md:block">
            Signed in as{" "}
            <span className="font-medium text-foreground">
              {user?.username ?? "?"}
            </span>{" "}
            ({user?.role ?? "?"})
          </div>
          <Button
            variant="outline"
            size="sm"
            className="w-full justify-center"
            onClick={logout}
            title={`Sign out ${user?.username ?? ""}`.trim()}
          >
            <LogOut className="h-4 w-4 md:mr-2" />
            <span className="hidden md:inline">Sign out</span>
          </Button>
        </div>
      </aside>
      <main className="flex-1 overflow-auto p-4 md:p-6">
        <Outlet />
      </main>
    </div>
  );
}
