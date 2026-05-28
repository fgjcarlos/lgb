import { UnavailableBanner } from "@/components/UnavailableBanner";

export function Tags() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Tags</h1>
      <UnavailableBanner message="Current tag table arrives in PR 3 (Frontend Dashboard + Tags)." />
    </div>
  );
}
