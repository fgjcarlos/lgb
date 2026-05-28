import { Badge } from "@/components/ui/badge";
import type { ConnectionStatus } from "@/hooks/useTagStream";

interface ConnectionBadgeProps {
  status: ConnectionStatus;
}

export function ConnectionBadge({ status }: ConnectionBadgeProps) {
  if (status === "connected") {
    return <Badge variant="success">Connected</Badge>;
  }
  if (status === "connecting") {
    return <Badge variant="warning">Connecting…</Badge>;
  }
  return <Badge variant="destructive">Disconnected</Badge>;
}
