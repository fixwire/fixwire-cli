import { createApp } from "vue";
import * as Sentry from "@sentry/vue";
import App from "./App.vue";
import router from "./router";

const app = createApp(App);

Sentry.init({
  app,
  dsn: import.meta.env.VITE_SENTRY_DSN,
  integrations: [Sentry.browserTracingIntegration({ router })],
  tracesSampleRate: 1,
  tracingOptions: { trackComponents: true },
});

app.use(router).mount("#app");
