import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "@/components/ui/button";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

// Top-level guard: a render crash shows a recoverable message, not a blank page.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("UI crashed", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert" className="mx-auto max-w-lg space-y-4 p-6">
        <h1 className="text-xl font-semibold">Something went wrong</h1>
        <p className="text-sm text-muted-foreground">
          The interface hit an unexpected error. The gateway keeps running;
          reload the page to continue.
        </p>
        <pre className="overflow-auto rounded bg-muted p-2 text-xs">
          {this.state.error.message}
        </pre>
        <Button onClick={() => window.location.reload()}>Reload</Button>
      </div>
    );
  }
}
