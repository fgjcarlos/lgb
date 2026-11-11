import { Badge } from "@/components/ui/badge";
import type { PLCConnectionState, PLCScanHealth } from "@/hooks/useApi";

export function PLCStateBadge({ state }: { state: PLCConnectionState }) {
  if (state === "connected") return <Badge variant="success">Connected</Badge>;
  if (state === "disconnected")
    return <Badge variant="destructive">Disconnected</Badge>;
  return <Badge variant="secondary">Never connected</Badge>;
}

export function ScanHealthBadge({ health }: { health: PLCScanHealth }) {
  if (health === "ok") return <Badge variant="success">Scanning</Badge>;
  if (health === "stale") return <Badge variant="warning">Stale</Badge>;
  return <Badge variant="outline">No scans yet</Badge>;
}

export function QualityBadge({ quality }: { quality: string | undefined }) {
  if (!quality) return <Badge variant="outline">no data</Badge>;
  if (quality === "good") return <Badge variant="success">good</Badge>;
  return <Badge variant="destructive">{quality}</Badge>;
}
