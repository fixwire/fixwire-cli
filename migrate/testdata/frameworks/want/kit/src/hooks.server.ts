import * as Sentry from "@fixwire/sveltekit";
import { sequence } from "@sveltejs/kit/hooks";

Sentry.init({ dsn: process.env.SENTRY_DSN, tracesSampleRate: 1 });

export const handle = sequence(Sentry.fixwireHandle());
export const handleError = Sentry.handleErrorWithFixwire();
