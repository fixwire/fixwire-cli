// Error monitoring starts before the app ("@sentry/node" in a comment stays).
import * as Sentry from "@sentry/node";
import { nodeProfilingIntegration } from "@sentry/profiling-node";

Sentry.init({
  dsn: "https://abc123@o450000.ingest.us.sentry.io/4500000",
  release: process.env.SENTRY_RELEASE,
  integrations: [nodeProfilingIntegration(), Sentry.httpIntegration()],
  tracesSampleRate: 1.0,
  profilesSampleRate: 1.0,
  sendDefaultPii: true,
});

export const label = `monitoring by ${"@sentry/node"}`;
