import type { ComponentType, ReactNode } from "react";
import {
  Activity,
  Database,
  Gauge,
  HardDriveDownload,
  Stethoscope,
  Tags as TagsIcon,
  Users as UsersIcon,
} from "lucide-react";
import type { AuthUser } from "@/lib/jwt";
import { Dashboard } from "@/pages/Dashboard";
import { Tags } from "@/pages/Tags";
import { Historian } from "@/pages/Historian";
import { Diagnostics } from "@/pages/Diagnostics";
import { Backup } from "@/pages/Backup";
import { Users } from "@/pages/Users";

export interface RouteEntry {
  path: string;
  label: string;
  icon: ComponentType<{ className?: string }>;
  requiredRole?: AuthUser["role"];
  element: ReactNode;
}

export const routes: RouteEntry[] = [
  {
    path: "/",
    label: "Dashboard",
    icon: Gauge,
    element: <Dashboard />,
  },
  {
    path: "/tags",
    label: "Tags",
    icon: TagsIcon,
    element: <Tags />,
  },
  {
    path: "/historian",
    label: "Historian",
    icon: Database,
    element: <Historian />,
  },
  {
    path: "/diagnostics",
    label: "Diagnostics",
    icon: Stethoscope,
    element: <Diagnostics />,
  },
  {
    path: "/backup",
    label: "Backup",
    icon: HardDriveDownload,
    requiredRole: "admin",
    element: <Backup />,
  },
  {
    path: "/users",
    label: "Users",
    icon: UsersIcon,
    requiredRole: "admin",
    element: <Users />,
  },
];

export const navIcon = Activity;
