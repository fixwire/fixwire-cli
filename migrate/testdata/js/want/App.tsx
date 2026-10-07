import * as Sentry from "@fixwire/browser";
import { ErrorBoundary } from "@fixwire/react";
import { createRoot } from "react-dom/client";

Sentry.init({
  dsn: import.meta.env.VITE_SENTRY_DSN,
  integrations: [Sentry.browserTracingIntegration()],
  tracesSampleRate: 0.2,
  tracePropagationTargets: ["localhost", /^https:\/\/api\.shop\.example/],
});

function App() {
  return (
    <ErrorBoundary fallback={<p>Something broke.</p>}>
      <main>Shop</main>
    </ErrorBoundary>
  );
}

createRoot(document.getElementById("root")!).render(<App />);
