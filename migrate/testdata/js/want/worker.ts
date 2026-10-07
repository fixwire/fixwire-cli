import * as Sentry from "@fixwire/edge";

interface Env {
  SENTRY_DSN: string;
}

export default Sentry.withFixwire(
  (env: Env) => ({
    dsn: env.SENTRY_DSN,
    tracesSampleRate: 1.0,
  }),
  {
    async fetch(request: Request): Promise<Response> {
      return new Response(`ok ${request.url}`);
    },
  },
);
