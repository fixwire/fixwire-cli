import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { pack } from "../pack.mjs";

const require = createRequire(import.meta.url);
const { PLATFORMS, packageFor, binaryName } = require("../cli/lib/platform.js");
const here = path.dirname(fileURLToPath(import.meta.url));
const shim = path.join(here, "..", "cli", "bin", "fixwire-cli.js");

// What GoReleaser builds (.goreleaser.yaml): every target has a package, and
// every package a target.
test("every target has a package", () => {
  const targets = [];
  for (const goos of ["linux", "darwin", "windows"]) {
    for (const goarch of ["amd64", "arm64"]) {
      targets.push(`${goos}/${goarch}`);
    }
  }
  const packaged = Object.values(PLATFORMS).map((p) => `${p.goos}/${p.goarch}`);
  assert.deepEqual(packaged.toSorted(), targets.toSorted());
  assert.equal(packageFor("darwin", "arm64"), "@fixwire/cli-darwin-arm64");
  assert.equal(packageFor("win32", "x64"), "@fixwire/cli-win32-x64");
  assert.equal(packageFor("linux", "ia32"), undefined);
  assert.equal(binaryName("win32"), "fixwire-cli.exe");
  assert.equal(binaryName("linux"), "fixwire-cli");
});

test("pack builds a package per platform and the main one", () => {
  const work = fs.mkdtempSync(path.join(os.tmpdir(), "fixwire-cli-pack-"));
  try {
    const dist = path.join(work, "dist");
    const artifacts = [];
    for (const { goos, goarch } of Object.values(PLATFORMS)) {
      const rel = `dist/fixwire-cli_${goos}_${goarch}/fixwire-cli${goos === "windows" ? ".exe" : ""}`;
      fs.mkdirSync(path.join(work, path.dirname(rel)), { recursive: true });
      fs.writeFileSync(path.join(work, rel), `binary for ${goos}/${goarch}`);
      artifacts.push({ name: path.basename(rel), path: rel, goos, goarch, type: "Binary" });
    }
    artifacts.push({ name: "checksums.txt", path: "dist/checksums.txt", type: "Checksum" });
    fs.writeFileSync(path.join(dist, "artifacts.json"), JSON.stringify(artifacts));

    const out = path.join(work, "npm");
    const dirs = pack("1.2.3", dist, out);
    assert.equal(dirs.length, 7);
    assert.equal(path.basename(dirs.at(-1)), "cli", "the main package comes last");

    const linux = JSON.parse(
      fs.readFileSync(path.join(out, "cli-linux-x64", "package.json"), "utf8"),
    );
    assert.equal(linux.name, "@fixwire/cli-linux-x64");
    assert.equal(linux.version, "1.2.3");
    assert.deepEqual([linux.os, linux.cpu], [["linux"], ["x64"]]);
    const bin = path.join(out, "cli-linux-x64", "bin", "fixwire-cli");
    assert.equal(fs.readFileSync(bin, "utf8"), "binary for linux/amd64");
    if (process.platform !== "win32") {
      assert.equal(fs.statSync(bin).mode & 0o111, 0o111, "the binary is executable");
    }
    assert.ok(fs.existsSync(path.join(out, "cli-win32-arm64", "bin", "fixwire-cli.exe")));
    assert.ok(fs.existsSync(path.join(out, "cli-darwin-arm64", "LICENSE")));

    const main = JSON.parse(fs.readFileSync(path.join(out, "cli", "package.json"), "utf8"));
    assert.equal(main.version, "1.2.3");
    assert.equal(Object.keys(main.optionalDependencies).length, 6);
    assert.ok(Object.values(main.optionalDependencies).every((v) => v === "1.2.3"));
    for (const file of ["README.md", "LICENSE", "bin/fixwire-cli.js", "lib/platform.js"]) {
      assert.ok(fs.existsSync(path.join(out, "cli", file)), file);
    }

    assert.throws(() => pack("latest", dist, out), /not a version/);
    fs.writeFileSync(path.join(dist, "artifacts.json"), JSON.stringify(artifacts.slice(1)));
    assert.throws(() => pack("1.2.3", dist, out), /no .* binary/);
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
});

// The launcher passes the arguments on and exits as the binary does.
test("the launcher runs the binary", () => {
  const env = { ...process.env, FIXWIRE_CLI_BINARY: process.execPath };
  const ok = spawnSync(
    process.execPath,
    [shim, "-e", "process.stdout.write(process.argv.slice(1).join(' '))", "a", "b"],
    {
      env,
      encoding: "utf8",
    },
  );
  assert.equal(ok.status, 0, ok.stderr);
  assert.equal(ok.stdout, "a b");
  const failed = spawnSync(process.execPath, [shim, "-e", "process.exit(3)"], { env });
  assert.equal(failed.status, 3);
});

test("the launcher says what is missing", () => {
  const env = { ...process.env };
  delete env.FIXWIRE_CLI_BINARY;
  const r = spawnSync(process.execPath, [shim, "--version"], { env, encoding: "utf8" });
  assert.equal(r.status, 1);
  assert.match(r.stderr, /isn't installed|no build for/);
});
