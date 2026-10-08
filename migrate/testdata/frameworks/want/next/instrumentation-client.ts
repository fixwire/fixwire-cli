import * as Sentry from "@fixwire/nextjs";

Sentry.init({
  dsn: process.env.NEXT_PUBLIC_SENTRY_DSN,
  integrations: [],
  tracesSampleRate: 1,
});

export const onRouterTransitionStart = Sentry.onRouterTransitionStart;
