#!/usr/bin/env node
// Runs the Go binary for this platform. The package holds one per platform.
const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const { join } = require("node:path");

const exe = process.platform === "win32" ? ".exe" : "";
const binary = join(__dirname, "bin", `slowplan-${process.platform}-${process.arch}${exe}`);
if (!existsSync(binary)) {
  console.error(`slowplan has no build for ${process.platform}-${process.arch}. Please open an issue at https://github.com/GillMacedo/slowplan/issues`);
  process.exit(2);
}
const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  console.error(`slowplan: ${result.error.message}`);
  process.exit(2);
}
process.exit(result.status ?? 2);
