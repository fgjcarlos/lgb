import { UnavailableBanner } from "@/components/UnavailableBanner";

export function Backup() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Backup</h1>
      <UnavailableBanner message="Backup trigger and snapshot list arrive in PR 5 (Frontend Admin Pages)." />
    </div>
  );
}
