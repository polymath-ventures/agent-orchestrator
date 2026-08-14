import { defineConfig } from "@playwright/test";

export function resolveE2EPort(env: { AO_E2E_PORT?: string } = process.env) {
	const parsed = Number(env.AO_E2E_PORT ?? "");
	return Number.isInteger(parsed) && parsed > 0 && parsed <= 65_535 ? parsed : 5173;
}

const port = resolveE2EPort();

export default defineConfig({
	testDir: "e2e",
	use: {
		baseURL: `http://127.0.0.1:${port}`,
	},
	webServer: {
		// Exercise the production browser bundle and same-origin server. Individual
		// specs provide daemon-shaped HTTP/WS fixtures so no local daemon is needed.
		command: `npm run build:web && AO_WEB_DIST=dist AO_WEB_PORT=${port} AO_WEB_PUBLIC_URL=http://127.0.0.1:${port} AO_WEB_API_TARGET=http://127.0.0.1:9 node ../ops/ao-web-server.mjs`,
		port,
		reuseExistingServer: false,
		timeout: 180_000,
	},
});
