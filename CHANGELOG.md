# Changelog

All notable changes to fixwire-cli are listed here. Versions follow
[Semantic Versioning](https://semver.org); before 1.0, a minor version may
change the commands.

## [0.1.0] - 2026-10-07

- `sourcemaps inject <dir>` stamps each JavaScript file that has a source map, and its map, with a debug id derived from its content; a `"use strict"` prologue or a hashbang stays first and the map's columns move with it.
- `sourcemaps upload <dir>` sends the files and maps as one deterministic bundle, in chunks the server doesn't have yet, so a second upload of the same build sends nothing; `--inject` stamps first, `--release`, `--dist` and `--url-prefix` match files without debug ids.
- `releases set-commit` records the commit and repository a release was built from (`git rev-parse HEAD` and the `origin` remote by default).
- Settings from flags or `FIXWIRE_URL`, `FIXWIRE_AUTH_TOKEN`, `FIXWIRE_ORG`, `FIXWIRE_PROJECT` and `FIXWIRE_RELEASE`; the API key never appears in the output.
- Binaries for Linux, macOS and Windows on x64 and arm64, `go install`, and the npm package `@fixwire/cli`.
