import { UnavailableBanner } from "@/components/UnavailableBanner";

export function Historian() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Historian</h1>
      <UnavailableBanner message="Historian query form arrives in PR 4 (Frontend Data Pages)." />
    </div>
  );
}
