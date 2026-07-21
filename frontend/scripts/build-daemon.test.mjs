import { describe, expect, it } from "vitest";
import { daemonBuildArgs } from "./build-daemon.mjs";

describe("daemonBuildArgs", () => {
	it("stamps the frontend package version into the bundled daemon", () => {
		const args = daemonBuildArgs("/tmp/ao", "0.0.1-nightly.202607202300");

		expect(args).toEqual([
			"build",
			"-ldflags",
			"-X github.com/aoagents/agent-orchestrator/backend/internal/cli.Version=0.0.1-nightly.202607202300",
			"-o",
			"/tmp/ao",
			"./cmd/ao",
		]);
	});
});
