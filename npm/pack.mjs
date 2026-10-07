// Builds the npm packages from a GoReleaser build: one package per platform
// holding its binary (npm installs only the one matching the system, by its
// os and cpu fields), and @fixwire/cli, which depends on them all as optional
// dependencies and runs the installed one.
//
//	node npm/pack.mjs <version> <GoReleaser's dist directory> <output directory>
//
// It prints the packages' directories, platforms first: publish them in that
// order, so @fixwire/cli never points at a version that isn't there yet.

import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const { PLATFORMS, binaryName } = require("./cli/lib/platform.js");

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.dirname(here); // the repository, for README.md and LICENSE
const repository = { type: "git", url: "git+https://github.com/fixwire/fixwire-cli.git" };

export function pack(version, dist, out) {
  if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error(`pack: "${version}" is not a version`);
  }
  // GoReleaser lists what it built, with paths relative to where it ran: the
  // directory above dist.
  const artifacts = JSON.parse(fs.readFileSync(path.join(dist, "artifacts.json"), "utf8"));
  const binaries = new Map();
  for (const a of artifacts) {
    if (a.type === "Binary") {
      binaries.set(`${a.goos}/${a.goarch}`, path.resolve(path.dirname(path.resolve(dist)), a.path));
    }
  }

  fs.rmSync(out, { recursive: true, force: true });
  const dirs = [];
  const optional = {};
  for (const [key, { pkg, goos, goarch }] of Object.entries(PLATFORMS)) {
    const built = binaries.get(`${goos}/${goarch}`);
    if (!built) {
      throw new Error(`pack: GoReleaser built no ${goos}/${goarch} binary`);
    }
    const [platform, arch] = key.split("-");
    const dir = path.join(out, pkg.slice("@fixwire/".length));
    const bin = path.join(dir, "bin", binaryName(platform));
    fs.mkdirSync(path.dirname(bin), { recursive: true });
    fs.copyFileSync(built, bin);
    fs.chmodSync(bin, 0o755);
    fs.copyFileSync(path.join(root, "LICENSE"), path.join(dir, "LICENSE"));
    writeJSON(path.join(dir, "package.json"), {
      name: pkg,
      version,
      description: `The fixwire-cli binary for ${platform} ${arch}; install @fixwire/cli instead.`,
      homepage: "https://github.com/fixwire/fixwire-cli",
      repository,
      license: "MIT",
      os: [platform],
      cpu: [arch],
      files: ["bin"],
      preferUnplugged: true,
    });
    optional[pkg] = version;
    dirs.push(dir);
  }

  const main = path.join(out, "cli");
  fs.cpSync(path.join(here, "cli"), main, { recursive: true });
  for (const file of ["README.md", "LICENSE"]) {
    fs.copyFileSync(path.join(root, file), path.join(main, file));
  }
  const manifest = JSON.parse(fs.readFileSync(path.join(main, "package.json"), "utf8"));
  writeJSON(path.join(main, "package.json"), {
    ...manifest,
    version,
    optionalDependencies: optional,
  });
  dirs.push(main);
  return dirs;
}

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [version, dist, out] = process.argv.slice(2);
  if (!version || !dist || !out) {
    process.stderr.write("usage: node npm/pack.mjs <version> <dist> <out>\n");
    process.exit(2);
  }
  for (const dir of pack(version.replace(/^v/, ""), dist, out)) {
    process.stdout.write(`${dir}\n`);
  }
}
