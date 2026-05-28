import { Info } from "lucide-react";
import { cn } from "@/lib/utils";

interface UnavailableBannerProps {
  className?: string;
  message?: string;
}

export function UnavailableBanner({ className, message }: UnavailableBannerProps) {
  return (
    <div
      role="status"
      className={cn(
        "flex items-start gap-3 rounded-md border border-amber-300 bg-amber-50 p-4 text-amber-900",
        "dark:border-amber-800 dark:bg-amber-950/40 dark:text-amber-100",
        className,
      )}
    >
      <Info className="mt-0.5 h-4 w-4 shrink-0" />
      <p className="text-sm">
        {message ?? "Endpoint unavailable — blocked on REST API rollout (Issue #14)."}
      </p>
    </div>
  );
}
