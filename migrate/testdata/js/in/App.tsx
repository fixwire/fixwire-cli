import * as Sentry from "@sentry/react";
import { createRoot } from "react-dom/client";

Sentry.init({
  dsn: import.meta.env.VITE_SENTRY_DSN,
  integrations: [Sentry.browserTracingIntegration(), Sentry.replayIntegration()],
  tracesSampleRate: 0.2,
  tracePropagationTargets: ["localhost", /^https:\/\/api\.shop\.example/],
  replaysSessionSampleRate: 0.1,
  replaysOnErrorSampleRate: 1.0,
});

function App() {
  return (
    <Sentry.ErrorBoundary fallback={<p>Something broke.</p>}>
      <main>Shop</main>
    </Sentry.ErrorBoundary>
  );
}

createRoot(document.getElementById("root")!).render(<App />);
