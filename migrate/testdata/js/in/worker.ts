import * as Sentry from "@sentry/cloudflare";

interface Env {
  SENTRY_DSN: string;
}

export default Sentry.withSentry(
  (env: Env) => ({
    dsn: env.SENTRY_DSN,
    tracesSampleRate: 1.0,
    enableLogs: true,
  }),
  {
    async fetch(request: Request): Promise<Response> {
      return new Response(`ok ${request.url}`);
    },
  },
);
