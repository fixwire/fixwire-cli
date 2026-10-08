import { ApplicationConfig, ErrorHandler, inject, provideAppInitializer } from "@angular/core";
import { provideRouter, Router } from "@angular/router";
import * as Sentry from "@sentry/angular";
import { routes } from "./app.routes";

export const appConfig: ApplicationConfig = {
  providers: [
    provideRouter(routes),
    { provide: ErrorHandler, useValue: Sentry.createErrorHandler({ showDialog: true }) },
    { provide: Sentry.TraceService, deps: [Router] },
    provideAppInitializer(() => {
      inject(Sentry.TraceService);
    }),
  ],
};
