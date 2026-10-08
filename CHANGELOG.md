# Changelog

All notable changes to fixwire-cli are listed here. Versions follow
[Semantic Versioning](https://semver.org); before 1.0, a minor version may
change the commands.

## [0.2.0] - 2026-10-08

- `migrate` moves projects on the other tracker's Next.js, Vue, SvelteKit and Angular packages to `@fixwire/nextjs`, `@fixwire/vue`, `@fixwire/sveltekit` and `@fixwire/angular` (its Svelte package to `@fixwire/browser`), renames the Next.js config wrapper, the router transition hook, SvelteKit's `handleError` wrapper, `handle` hook and Vite plugin, and says what changed where a call takes less (the config wrapper's second argument, the Vite plugin's upload options, Angular's report dialog) and what replaces what Fixwire lacks (Angular's `TraceService`: `provideFixwire()`). Nuxt gets precise steps: the module, the config files, the DSN. Environment variables keep a framework's public prefix (`NEXT_PUBLIC_FIXWIRE_DSN`, `VITE_FIXWIRE_DSN`).
- `sourcemaps upload --delete` removes the source maps once they're uploaded, so they aren't deployed with the build.
- `sourcemaps inject` keeps a build's precompressed copies in step with the files it stamps, as servers send them in their place (SvelteKit's adapter-node does): `<file>.gz` is rewritten, and `<file>.br` removed, since there is no brotli encoder in the standard library; servers fall back to the gzip copy.
- `sourcemaps inject` adds the snippet that reports a debug id at run time to files whose bundler already wrote one (esbuild in Angular's builder, Rollup's `sourcemapDebugIds`), keeping the bundler's id; such files were taken for stamped ones, and their errors arrived without debug ids.

## [0.1.0] - 2026-10-08

- `migrate [--write] <dir>` moves a JavaScript, TypeScript or Python project from another error tracker's SDK to Fixwire's: imports, renamed calls, options and integrations Fixwire doesn't have, and the dependencies in package.json, requirements files, pyproject.toml, Pipfile and setup.cfg; Django gets Fixwire's middleware. It lists what's left to do by hand, never touches comments or strings, and a second run changes nothing.

- `sourcemaps inject <dir>` stamps each JavaScript file that has a source map, and its map, with a debug id derived from its content; a `"use strict"` prologue or a hashbang stays first and the map's columns move with it.
- `sourcemaps upload <dir>` sends the files and maps as one deterministic bundle, in chunks the server doesn't have yet, so a second upload of the same build sends nothing; `--inject` stamps first, `--release`, `--dist` and `--url-prefix` match files without debug ids.
- `releases set-commit` records the commit and repository a release was built from (`git rev-parse HEAD` and the `origin` remote by default).
- Settings from flags or `FIXWIRE_URL`, `FIXWIRE_AUTH_TOKEN`, `FIXWIRE_ORG`, `FIXWIRE_PROJECT` and `FIXWIRE_RELEASE`; the API key never appears in the output.
- Binaries for Linux, macOS and Windows on x64 and arm64, `go install`, and the npm package `@fixwire/cli`.
