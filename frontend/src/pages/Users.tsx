import { UnavailableBanner } from "@/components/UnavailableBanner";

export function Users() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Users</h1>
      <UnavailableBanner message="User CRUD UI arrives in PR 5 (Frontend Admin Pages)." />
    </div>
  );
}
