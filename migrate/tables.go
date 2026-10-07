package migrate

import "strings"

// The Fixwire versions a migrated project depends on. tables_test.go checks
// them, and the names below, against the SDKs in this repository.
const (
	jsVersion = "0.1.3"
	pyVersion = "0.1.2"
)

// --- JavaScript ------------------------------------------------------------

// jsTarget is the Fixwire package an incumbent package becomes. React's is
// split: its three React parts come from @fixwire/react, the rest (init,
// capture…) from @fixwire/browser.
type jsTarget struct {
	pkg   string
	react bool
}

// jsPackages maps the incumbent's packages to Fixwire's.
var jsPackages = map[string]jsTarget{
	"@sentry/node":           {pkg: "@fixwire/node"},
	"@sentry/aws-serverless": {pkg: "@fixwire/node"},
	"@sentry/browser":        {pkg: "@fixwire/browser"},
	"@sentry/react":          {pkg: "@fixwire/browser", react: true},
	"@sentry/core":           {pkg: "@fixwire/core"},
	"@sentry/cloudflare":     {pkg: "@fixwire/edge"},
	"@sentry/vercel-edge":    {pkg: "@fixwire/edge"},
}

// jsReactExports are @fixwire/react's names.
var jsReactExports = set("ErrorBoundary", "withErrorBoundary", "reactErrorHandler")

// jsDropped are incumbent packages with nothing in Fixwire to move to:
// their imports, and the integrations they made, are removed.
var jsDropped = map[string]string{
	"@sentry/profiling-node":  "Fixwire has no profiling",
	"@sentry/replay":          "Fixwire has no session replay",
	"@sentry-internal/replay": "Fixwire has no session replay",
}

// jsByHand are incumbent packages a person moves: why, and how.
var jsByHand = map[string]string{
	"@sentry/nextjs":                  "Next.js: use @fixwire/node in instrumentation.ts (export onRequestError = captureRequestError) and @fixwire/browser in the client; upload source maps with `fixwire-cli sourcemaps upload --inject` instead of the config wrapper",
	"@sentry/nuxt":                    "Nuxt: use @fixwire/browser in a client plugin and @fixwire/node on the server",
	"@sentry/vue":                     "Vue: use @fixwire/browser, and report from app.config.errorHandler with captureException",
	"@sentry/angular":                 "Angular: use @fixwire/browser, and report from an ErrorHandler with captureException",
	"@sentry/svelte":                  "Svelte: use @fixwire/browser",
	"@sentry/sveltekit":               "SvelteKit: use @fixwire/browser in hooks.client and @fixwire/node in hooks.server (handleError)",
	"@sentry/remix":                   "Remix: use @fixwire/browser on the client and @fixwire/node on the server",
	"@sentry/astro":                   "Astro: use @fixwire/browser and @fixwire/node",
	"@sentry/solid":                   "Solid: use @fixwire/browser",
	"@sentry/ember":                   "Ember: use @fixwire/browser",
	"@sentry/gatsby":                  "Gatsby: use @fixwire/browser",
	"@sentry/bun":                     "Bun: use @fixwire/node",
	"@sentry/deno":                    "Deno: use @fixwire/edge (wrapRequestHandler)",
	"@sentry/google-cloud-serverless": "Google Cloud Functions: use @fixwire/node (wrapHandler)",
	"@sentry/electron":                "Electron isn't supported yet; @fixwire/node covers the main process",
	"@sentry/react-native":            "React Native comes with Fixwire for Mobile",
	"@sentry/capacitor":               "Capacitor comes with Fixwire for Mobile",
	"@sentry/webpack-plugin":          "remove the bundler plugin and upload source maps in CI with `fixwire-cli sourcemaps upload --inject <dir>`",
	"@sentry/vite-plugin":             "remove the bundler plugin and upload source maps in CI with `fixwire-cli sourcemaps upload --inject <dir>`",
	"@sentry/rollup-plugin":           "remove the bundler plugin and upload source maps in CI with `fixwire-cli sourcemaps upload --inject <dir>`",
	"@sentry/esbuild-plugin":          "remove the bundler plugin and upload source maps in CI with `fixwire-cli sourcemaps upload --inject <dir>`",
	"@sentry/cli":                     "use fixwire-cli: `sourcemaps upload --inject <dir>` and `releases set-commit`",
	"@sentry/wizard":                  "remove it; fixwire-cli migrate did its part",
}

// jsRenamed are the incumbent's names Fixwire calls otherwise.
var jsRenamed = map[string]string{
	"withSentry": "withFixwire",
}

// jsExports are each Fixwire package's names (core's are in every other but
// react). Anything else used from a migrated import is reported.
var jsCore = set(
	"AgentSpan", "ChatSpan", "Client", "DEFAULT_DETECTORS", "Delivery", "FILTERED", "Limiter", "MAX_AI_CONTENT",
	"MAX_SPANS_PER_SEGMENT", "Redactor", "SDK_VERSION", "Scope", "Span", "ToolSpan", "UNKNOWN_FUNCTION",
	"addBreadcrumb", "ai", "argumentsHash", "bindClient", "captureCheckIn", "captureEvent", "captureException",
	"captureFeedback", "captureMessage", "close", "continueTrace", "createStackParser", "debugImages",
	"eventFromUnknown", "exceptionsFromError", "filenameIsInApp", "fingerprint", "flush", "getActiveSpan",
	"getClient", "getCurrentScope", "getGlobalScope", "getIsolationScope", "getPropagationContext", "getTraceData",
	"getTraceMetaTags", "hasTracingEnabled", "lastEventId", "newPropagationContext", "newSpanId", "newTraceId",
	"nodeStackLineParser", "normalize", "openTelemetryIntegration", "otlpExporterOptions", "parseDsn",
	"parseRateLimits", "propagationFromHeaders", "resolveIntegrations", "setAsyncContextStrategy", "setContext",
	"setExtra", "setTag", "setTags", "setUser", "shouldPropagate", "startInactiveSpan", "startSpan", "traceHeaders",
	"withActiveSpan", "withIsolationScope", "withScope", "wrapAnthropic", "wrapOpenAI",
	// Types the incumbent's users import too.
	"Event", "Breadcrumb", "User", "Integration", "SeverityLevel",
)

var jsExports = map[string]map[string]bool{
	"@fixwire/core": jsCore,
	"@fixwire/browser": union(jsCore, set("breadcrumbsIntegration", "browserPlatform", "browserTracingIntegration",
		"defaultStackParser", "globalHandlersIntegration", "init", "makeFetchTransport", "noiseFilter", "BrowserOptions")),
	"@fixwire/node": union(jsCore, set("captureRequestError", "defaultIntegrations", "expressErrorHandler",
		"fetchIntegration", "httpClientIntegration", "httpServerIntegration", "init", "makeFileSpool", "nodePlatform",
		"onUncaughtExceptionIntegration", "onUnhandledRejectionIntegration", "requestInfo", "safeHeaders",
		"setupExpressErrorHandler", "wrapHandler", "NodeOptions")),
	"@fixwire/edge": union(jsCore, set("edgePlatform", "fetchIntegration", "init", "makeEdgeTransport", "withFixwire",
		"wrapRequestHandler", "EdgeOptions")),
	"@fixwire/react": jsReactExports,
}

// jsOptions are the options Fixwire's init takes, per package; the others
// are removed (TypeScript refuses unknown ones).
var jsClientOptions = set("dsn", "release", "environment", "dist", "serverName", "sampleRate", "maxBreadcrumbs",
	"maxValueLength", "maxStackFrames", "maxQueue", "beforeSend", "beforeBreadcrumb", "sendDefaultPii", "redact",
	"sensitiveKeys", "rateLimit", "integrations", "defaultIntegrations", "transport", "offline", "debug",
	"tracesSampleRate", "tracesSampler", "tracePropagationTargets", "recordAiContent", "autoSessionTracking")

var jsOptions = map[string]map[string]bool{
	"@fixwire/core":    jsClientOptions,
	"@fixwire/browser": union(jsClientOptions, set("filterNoise")),
	"@fixwire/node":    union(jsClientOptions, set("useEnvironment")),
	"@fixwire/edge":    union(jsClientOptions, set("useEnvironment", "asyncLocalStorage")),
}

// jsOptionNotes say what became of an option Fixwire doesn't take.
var jsOptionNotes = map[string]string{
	"profilesSampleRate":       "Fixwire has no profiling",
	"profileSessionSampleRate": "Fixwire has no profiling",
	"profileLifecycle":         "Fixwire has no profiling",
	"replaysSessionSampleRate": "Fixwire has no session replay",
	"replaysOnErrorSampleRate": "Fixwire has no session replay",
	"ignoreErrors":             "drop those events in beforeSend instead",
	"ignoreTransactions":       "drop those spans in tracesSampler instead",
	"denyUrls":                 "filter them in the project's inbound filters, or in beforeSend",
	"allowUrls":                "list the sites in the project's allowed origins, or filter in beforeSend",
	"beforeSendTransaction":    "spans go through tracesSampler",
	"tunnel":                   "Fixwire's browser SDK sends without a CORS preflight, so no tunnel is needed",
	"_experiments":             "",
	"enableLogs":               "send logs with OpenTelemetry's OTLP exporter (otlpExporterOptions)",
	"spotlight":                "",
	"normalizeDepth":           "",
	"attachStacktrace":         "",
	"initialScope":             "set them after init with setUser, setTag and setContext",
}

// --- Python ----------------------------------------------------------------

// pyExports are the fixwire module's names.
var pyExports = set("AsyncClient", "Client", "Options", "RateLimit", "Scope", "Span", "__version__", "aclose",
	"add_breadcrumb", "aflush", "ai", "capture_check_in", "capture_event", "capture_exception", "capture_feedback",
	"capture_message", "close", "continue_trace", "current_span", "flush", "get_client", "get_current_scope",
	"get_global_scope", "get_isolation_scope", "init", "isolation_scope", "last_event_id", "new_scope",
	"serverless_function", "set_context", "set_extra", "set_level", "set_tag", "set_tags", "set_user",
	"should_propagate", "start_span", "trace_headers", "use_span", "integrations")

// pyRenamed are the incumbent's names Fixwire calls otherwise.
var pyRenamed = map[string]string{
	"capture_checkin": "capture_check_in",
}

// pyOptions are what fixwire.init takes besides the DSN.
var pyOptions = set("dsn", "release", "environment", "dist", "server_name", "sample_rate", "traces_sample_rate",
	"traces_sampler", "trace_propagation_targets", "max_breadcrumbs", "before_send", "before_breadcrumb",
	"include_local_variables", "include_source_context", "max_value_length", "max_stack_frames", "in_app_include",
	"in_app_exclude", "project_root", "redact", "sensitive_keys", "send_default_pii", "rate_limit", "transport",
	"shutdown_timeout", "http_timeout", "max_queue_size", "offline", "default_integrations", "integrations", "debug",
	"record_ai_content", "auto_session_tracking")

var pyOptionNotes = map[string]string{
	"profiles_sample_rate":        "Fixwire has no profiling",
	"profile_session_sample_rate": "Fixwire has no profiling",
	"profiles_sampler":            "Fixwire has no profiling",
	"enable_tracing":              "set traces_sample_rate instead",
	"ignore_errors":               "drop those events in before_send instead",
	"before_send_transaction":     "spans go through traces_sampler",
	"attach_stacktrace":           "",
	"http_proxy":                  "Fixwire reads HTTPS_PROXY from the environment",
	"https_proxy":                 "Fixwire reads HTTPS_PROXY from the environment",
	"enable_logs":                 "Fixwire's LoggingIntegration turns log records into breadcrumbs and events",
	"_experiments":                "",
	"spotlight":                   "",
	"max_request_body_size":       "",
	"socket_options":              "",
	"keep_alive":                  "",
}

// pyIntegration is what becomes of one of the incumbent's integration
// modules: a Fixwire one (module, and its constructor's keyword arguments),
// or nothing, with what to do instead.
type pyIntegration struct {
	module string          // fixwire.integrations.<module>; "" when there's none
	args   map[string]bool // the keyword arguments Fixwire's takes
	note   string          // when there's none: what to do
}

var pyIntegrations = map[string]pyIntegration{
	"logging":       {module: "logging", args: set("level", "event_level")},
	"celery":        {module: "celery", args: set()},
	"httpx":         {module: "httpx", args: set()},
	"opentelemetry": {module: "opentelemetry", args: set()},
	"django":        {note: "add \"fixwire.integrations.django.FixwireMiddleware\" first in MIDDLEWARE"},
	"flask":         {note: "call fixwire.integrations.flask.init_app(app) once the app exists"},
	"fastapi":       {note: "add the ASGI middleware: app.add_middleware(fixwire.integrations.asgi.FixwireMiddleware)"},
	"starlette":     {note: "add the ASGI middleware: app.add_middleware(fixwire.integrations.asgi.FixwireMiddleware)"},
	"asgi":          {note: "wrap the app: app = fixwire.integrations.asgi.FixwireMiddleware(app)"},
	"wsgi":          {note: "wrap the app: app = fixwire.integrations.wsgi.FixwireMiddleware(app)"},
	"stdlib":        {note: "for outgoing requests, add fixwire.integrations.requests.RequestsIntegration() or HttpxIntegration()"},
	"openai":        {note: "wrap the client: client = fixwire.ai.wrap_openai(OpenAI())"},
	"anthropic":     {note: "wrap the client: client = fixwire.ai.wrap_anthropic(Anthropic())"},
}

// pyRequirement matches a requirement on the incumbent's package: its
// name, any extras and a version specifier.
func isIncumbentRequirement(name string) bool {
	n := strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	return n == "sentry-sdk"
}

// asyncExtras are the incumbent's extras of async frameworks: Fixwire's
// async client needs fixwire[async] there.
var asyncExtras = set("fastapi", "starlette", "aiohttp", "asyncio", "httpx", "quart", "sanic", "litestar", "starlite",
	"asyncpg", "arq", "tornado")

// --- shared ----------------------------------------------------------------

func set(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func union(a, b map[string]bool) map[string]bool {
	m := make(map[string]bool, len(a)+len(b))
	for k := range a {
		m[k] = true
	}
	for k := range b {
		m[k] = true
	}
	return m
}
