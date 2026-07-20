import { defineConfig } from "@playwright/test";

export default defineConfig({
	testDir: "e2e",
	use: {
		baseURL: "http://127.0.0.1:5173",
	},
	webServer: {
		// Exercise the production browser bundle and same-origin server. Individual
		// specs provide daemon-shaped HTTP/WS fixtures so no local daemon is needed.
		command:
			"npm run build:web && AO_WEB_DIST=dist AO_WEB_PUBLIC_URL=http://127.0.0.1:5173 AO_WEB_API_TARGET=http://127.0.0.1:9 node ../ops/ao-web-server.mjs",
		port: 5173,
		reuseExistingServer: false,
		timeout: 180_000,
	},
});
