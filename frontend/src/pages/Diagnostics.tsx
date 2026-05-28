import { UnavailableBanner } from "@/components/UnavailableBanner";

export function Diagnostics() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Diagnostics</h1>
      <UnavailableBanner message="Doctor check results arrive in PR 4 (Frontend Data Pages)." />
    </div>
  );
}
