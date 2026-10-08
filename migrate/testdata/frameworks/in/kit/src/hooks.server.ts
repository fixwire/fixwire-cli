import * as Sentry from "@sentry/sveltekit";
import { sequence } from "@sveltejs/kit/hooks";

Sentry.init({ dsn: process.env.SENTRY_DSN, tracesSampleRate: 1 });

export const handle = sequence(Sentry.sentryHandle());
export const handleError = Sentry.handleErrorWithSentry();
