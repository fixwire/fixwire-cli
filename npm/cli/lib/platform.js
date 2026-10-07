// The npm package of each platform fixwire-cli is built for (by Node's
// names: process.platform and process.arch), and the Go target it holds.
const PLATFORMS = {
  "darwin-arm64": { pkg: "@fixwire/cli-darwin-arm64", goos: "darwin", goarch: "arm64" },
  "darwin-x64": { pkg: "@fixwire/cli-darwin-x64", goos: "darwin", goarch: "amd64" },
  "linux-arm64": { pkg: "@fixwire/cli-linux-arm64", goos: "linux", goarch: "arm64" },
  "linux-x64": { pkg: "@fixwire/cli-linux-x64", goos: "linux", goarch: "amd64" },
  "win32-arm64": { pkg: "@fixwire/cli-win32-arm64", goos: "windows", goarch: "arm64" },
  "win32-x64": { pkg: "@fixwire/cli-win32-x64", goos: "windows", goarch: "amd64" },
};

// packageFor is the package holding the binary for a platform and an
// architecture, undefined when there is none.
function packageFor(platform, arch) {
  return PLATFORMS[`${platform}-${arch}`]?.pkg;
}

// binaryName is the binary's file name on a platform.
function binaryName(platform) {
  return platform === "win32" ? "fixwire-cli.exe" : "fixwire-cli";
}

module.exports = { PLATFORMS, packageFor, binaryName };
