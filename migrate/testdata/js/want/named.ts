import { captureException, withScope, type Event } from "@fixwire/browser";
import { init } from "@fixwire/browser";
import { ErrorBoundary } from "@fixwire/react";

export function report(error: unknown, event?: Event): void {
  withScope((scope) => {
    scope.setTag("area", "checkout");
    captureException(error);
  });
  init({ dsn: "" });
  void ErrorBoundary;
  void event;
}
