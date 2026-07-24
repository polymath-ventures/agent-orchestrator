import { mkdirSync, readFileSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { meetsMinimumVersion, parseGoVersion, parseMinimumGoVersion } from "./go-version.mjs";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = resolve(scriptsDir, "..");
const repoRoot = resolve(frontendRoot, "..");
const backendRoot = join(repoRoot, "backend");
const outDir = join(frontendRoot, "daemon");
const outPath = join(outDir, process.platform === "win32" ? "ao.exe" : "ao");
const cliVersionSymbol = "github.com/aoagents/agent-orchestrator/backend/internal/cli.Version";
const minimumGoVersion = parseMinimumGoVersion(readFileSync(join(backendRoot, "go.mod"), "utf8"));

if (!minimumGoVersion) {
	console.error("Could not determine the required Go version from backend/go.mod.");
	process.exit(1);
}

const versionResult = spawnSync("go", ["version"], { encoding: "utf8" });
if (versionResult.error) {
	console.error(
		`Go ${minimumGoVersion.join(".")}+ is required, but Go could not be started: ${versionResult.error.message}`,
	);
	process.exit(1);
}
const actualGoVersion = parseGoVersion(versionResult.stdout);
if (versionResult.status !== 0 || !actualGoVersion || !meetsMinimumVersion(actualGoVersion, minimumGoVersion)) {
	const found = actualGoVersion ? actualGoVersion.join(".") : versionResult.stdout.trim() || "unknown";
	console.error(`Go ${minimumGoVersion.join(".")}+ required, found ${found} — upgrade at https://go.dev/dl/`);
	process.exit(1);
}

export function daemonBuildArgs(outputPath, version) {
	if (typeof version !== "string" || version.trim() === "") {
		throw new Error("build-daemon: frontend/package.json must contain a non-empty version");
	}
	return ["build", "-ldflags", `-X ${cliVersionSymbol}=${version}`, "-o", outputPath, "./cmd/ao"];
}

export function buildDaemon() {
	rmSync(outDir, { recursive: true, force: true });
	mkdirSync(outDir, { recursive: true });

	// Release/nightly workflows stamp package.json before Forge runs this
	// script. Use that authoritative package version for the bundled daemon;
	// direct `go build ./cmd/ao` remains unstamped and reports "dev".
	const { version } = JSON.parse(readFileSync(join(frontendRoot, "package.json"), "utf8"));
	const result = spawnSync("go", daemonBuildArgs(outPath, version), {
		cwd: backendRoot,
		stdio: "inherit",
	});

	if (result.error) {
		console.error(`failed to start go build: ${result.error.message}`);
		process.exit(1);
	}

	if (result.status !== 0) {
		process.exit(result.status ?? 1);
	}
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
	buildDaemon();
}
