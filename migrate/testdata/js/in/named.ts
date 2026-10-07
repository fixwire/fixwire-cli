import { captureException, withScope, type Event } from "@sentry/browser";
import { ErrorBoundary, init } from "@sentry/react";

export function report(error: unknown, event?: Event): void {
  withScope((scope) => {
    scope.setTag("area", "checkout");
    captureException(error);
  });
  init({ dsn: "", ignoreErrors: ["ResizeObserver loop"] });
  void ErrorBoundary;
  void event;
}
