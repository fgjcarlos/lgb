import { UnavailableBanner } from "@/components/UnavailableBanner";

export function Dashboard() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Dashboard</h1>
      <UnavailableBanner message="Realtime tag stream and charts arrive in PR 3 (Frontend Dashboard + Tags)." />
    </div>
  );
}
