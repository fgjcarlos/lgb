import { useEffect, type ReactNode } from "react";

// Sets the browser tab title for the page it wraps.
export function PageTitle({ title, children }: { title: string; children: ReactNode }) {
  useEffect(() => {
    document.title = `${title} · LGB`;
  }, [title]);
  return children;
}
