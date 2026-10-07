#!/usr/bin/env node

// Runs the fixwire-cli binary of this platform's package, which npm
// installed as an optional dependency (only the one matching this system).
// No install script downloads anything, so it works where scripts are off.

const { spawnSync } = require("node:child_process");
const path = require("node:path");
const { packageFor, binaryName } = require("../lib/platform.js");

function fail(message) {
  process.stderr.write(`fixwire-cli: ${message}\n`);
  process.exit(1);
}

function binary() {
  // A binary of your own (a source build, a mirror) wins.
  if (process.env.FIXWIRE_CLI_BINARY) {
    return process.env.FIXWIRE_CLI_BINARY;
  }
  const pkg = packageFor(process.platform, process.arch);
  if (!pkg) {
    fail(
      `no build for ${process.platform} ${process.arch}; ` +
        "download one from https://github.com/fixwire/fixwire-cli/releases or `go install github.com/fixwire/fixwire-cli@latest`",
    );
  }
  try {
    return path.join(
      path.dirname(require.resolve(`${pkg}/package.json`)),
      "bin",
      binaryName(process.platform),
    );
  } catch {
    fail(`${pkg} isn't installed: reinstall without --no-optional or --omit=optional`);
  }
}

const result = spawnSync(binary(), process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  fail(result.error.message);
}
if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status ?? 1);
