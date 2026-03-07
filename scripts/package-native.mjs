#!/usr/bin/env node

import { copyFileSync, existsSync, mkdirSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(scriptDir, "..");
const binDir = resolve(repoRoot, "bin");
const distNativeDir = resolve(repoRoot, "dist", "native");

if (!existsSync(binDir)) {
  throw new Error(`Missing native build directory: ${binDir}`);
}

const nativeArtifacts = readdirSync(binDir).filter((name) => name.startsWith("libeasymatrixffi."));
if (nativeArtifacts.length === 0) {
  throw new Error(`No native artifacts found in ${binDir}. Run ./build-noweb.sh first.`);
}

mkdirSync(distNativeDir, { recursive: true });
for (const artifact of nativeArtifacts) {
  copyFileSync(resolve(binDir, artifact), resolve(distNativeDir, artifact));
}
