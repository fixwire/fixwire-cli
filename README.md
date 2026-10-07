<div align="center">

_Bugs reach production. Fixwire finds them first: errors, traces, logs and
AI agent runs in one place, an AI debugger on every plan, and your data
kept in Europe._

[![Release](https://img.shields.io/github/v/release/fixwire/fixwire-cli?label=release)](https://github.com/fixwire/fixwire-cli/releases)
[![npm](https://img.shields.io/npm/v/@fixwire/cli?label=npm)](https://www.npmjs.com/package/@fixwire/cli)
[![CI](https://github.com/fixwire/fixwire-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/fixwire/fixwire-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/fixwire/fixwire-cli/blob/main/LICENSE)

<br/>

</div>

# fixwire-cli

The command line tool for **[Fixwire](https://fixwire.io)**, run from your
build or CI:

- **Source maps:** stamps each JavaScript file and its source map with a
  debug id, then uploads them, so Fixwire shows your original code in stack
  traces.
- **Releases:** records the commit a release was built from, so the AI
  debugger reads the right source and the diff from the previous release.

It is one static binary with no dependencies.

## 📦 Install

```sh
npm install --save-dev @fixwire/cli      # or pnpm add -D, yarn add -D; then npx fixwire-cli
go install github.com/fixwire/fixwire-cli@latest
```

Or download a binary for Linux, macOS or Windows (x64 and arm64) from the
[releases](https://github.com/fixwire/fixwire-cli/releases), and check it
against `checksums.txt`.

## ⚙️ Settings

Flags win over the environment:

| Flag | Environment | |
|---|---|---|
| `--url` | `FIXWIRE_URL` | Your Fixwire API, e.g. `https://api.eu.fixwire.io` |
| `--auth-token` | `FIXWIRE_AUTH_TOKEN` | An API key with the `artifacts:write` scope (source maps) or `releases:write` (releases) |
| `--org` | `FIXWIRE_ORG` | Optional: the key names its organization |
| `--project` | `FIXWIRE_PROJECT` | Optional: a key writes only to its own project; several, comma-separated |
| `--release` | `FIXWIRE_RELEASE` | The release, as your SDK reports it |

The API key never appears in the tool's output, its usage text included.

## 🗺️ Source maps

After your build, before you deploy:

```sh
fixwire-cli sourcemaps inject dist
fixwire-cli sourcemaps upload dist
```

`inject` gives each built file that has a source map a debug id derived
from its content, in the file and in its map (a `"use strict"` prologue or
a hashbang stays first, and the map's columns move with it). Fixwire's
browser and Node SDKs report the debug ids with every error, so stack
traces find their maps whatever the URL or the release.

`upload` sends every file and map in the directory as one bundle. A file
already uploaded isn't sent again, so uploading the same build twice sends
nothing. `--inject` stamps the files first. Without debug ids, pass
`--release` (and `--dist`): frames are then matched by release and URL,
with `--url-prefix` the path the directory is served under (default `~/`,
any host).

## 🏷️ Releases

```sh
fixwire-cli releases set-commit --release web@1.4.0
```

records the commit at `git rev-parse HEAD` and the repository of the
`origin` remote; `--commit` and `--repository owner/name` set them
yourself.

## 🤖 In CI

GitHub Actions, with `FIXWIRE_AUTH_TOKEN` as a secret:

```yaml
- run: npm run build
- run: npx fixwire-cli sourcemaps upload --inject dist
  env:
    FIXWIRE_URL: https://api.eu.fixwire.io
    FIXWIRE_AUTH_TOKEN: ${{ secrets.FIXWIRE_AUTH_TOKEN }}
    FIXWIRE_RELEASE: web@${{ github.sha }}
- run: npx fixwire-cli releases set-commit
  env:
    FIXWIRE_URL: https://api.eu.fixwire.io
    FIXWIRE_AUTH_TOKEN: ${{ secrets.FIXWIRE_AUTH_TOKEN }}
    FIXWIRE_RELEASE: web@${{ github.sha }}
```

## Contributing

Issues and pull requests are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md).
Report security issues privately: see [SECURITY.md](SECURITY.md).

## License

MIT
