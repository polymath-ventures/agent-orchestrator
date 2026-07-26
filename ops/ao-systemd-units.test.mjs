import { readFile } from "node:fs/promises";
import test from "node:test";
import assert from "node:assert/strict";

async function unit(path) {
	return readFile(new URL(path, import.meta.url), "utf8");
}

test("ao.service is a persistent headless daemon that does not kill the tmux fleet on restart", async () => {
	const text = await unit("./ao.service");

	assert.match(text, /^ExecStart=%h\/\.local\/bin\/ao daemon$/m);
	assert.match(text, /^Restart=always$/m);
	assert.match(text, /^KillMode=process$/m);
	assert.match(text, /^After=.*ao-tmux\.service/m);
	assert.match(text, /^Requires=.*ao-tmux\.service/m);
	assert.match(text, /^StartLimitIntervalSec=0$/m);
	assert.doesNotMatch(text, /^Delegate=yes$/m);
	assert.match(text, /^ExecStartPre=.*command -v tmux/m);
	// The ownership gate must probe the socket the daemon actually uses
	// (AO_DATA_DIR/run/tmux/default, issue #160). Probing tmux's default socket
	// would pass while protecting nothing.
	assert.match(text, /^ExecStartPre=.*tmux -S %h\/\.ao\/data\/run\/tmux\/default list-sessions/m);
	// ...and it must still accept a server on the legacy default socket, because
	// deploy.sh restarts ao.service but cannot restart ao-tmux.service
	// (RefuseManualStop=yes). Without this the socket move bricks the daemon on
	// the deploy that introduces it. Match the branch's own distinguishing text:
	// a "socket-less list-sessions" regex cannot work, because the character
	// before " list-sessions" in the -S form is the "t" of "default".
	assert.match(
		text,
		/if tmux list-sessions >\/dev\/null 2>&1; then echo "ao\.service: no server on AO tmux socket yet/,
	);
	// Data dir must be ~/.ao/data — NEVER the ~/.ao root: the root is the
	// legacy layout, and pointing the daemon there resurrects a decommissioned
	// deployment's database wholesale (the 2026-07-21 incident).
	assert.match(text, /^Environment=AO_DATA_DIR=%h\/\.ao\/data$/m);
});

test("ao-tmux.service owns AO's tmux socket and refuses stop-job fleet kills", async () => {
	const text = await unit("./ao-tmux.service");

	assert.match(text, /^Before=ao\.service$/m);
	assert.match(text, /^RefuseManualStop=yes$/m);
	assert.match(text, /^Type=forking$/m);
	// The server this unit owns must be on the socket the daemon uses, not
	// tmux's default one (issue #160) — otherwise the daemon lazily forks its
	// own server into ao.service's cgroup and a routine restart kills the fleet.
	assert.match(text, /^ExecStart=.*tmux -S %h\/\.ao\/data\/run\/tmux\/default start-server.*exit-empty off/m);
	// tmux will not bind a socket whose directory is missing; 0700 matches
	// tmux's own /tmp/tmux-$UID.
	assert.match(text, /^ExecStartPre=.*install -d -m 700 %h\/\.ao\/data\/run\/tmux$/m);
	assert.match(text, /^ExecCondition=.*tmux -S %h\/\.ao\/data\/run\/tmux\/default list-sessions/m);
	assert.doesNotMatch(text, /^ExecStop=/m);
	assert.match(text, /^KillMode=process$/m);
	assert.match(text, /^KillSignal=SIGCONT$/m);
	assert.match(text, /^SendSIGKILL=no$/m);
	assert.match(text, /^Restart=always$/m);
});

test("ao-tmux-claim timer retries ownership without blocking daemon startup", async () => {
	const service = await unit("./ao-tmux-claim.service");
	const timer = await unit("./ao-tmux-claim.timer");

	assert.match(service, /^Type=oneshot$/m);
	assert.match(service, /^ExecStart=.*systemctl --user start --no-block ao-tmux\.service$/m);
	assert.match(timer, /^OnBootSec=30s$/m);
	assert.match(timer, /^OnUnitInactiveSec=30s$/m);
	assert.match(timer, /^Unit=ao-tmux-claim\.service$/m);
});

test("ao-config-drift service is a oneshot that runs the drift-check runner and never applies", async () => {
	const service = await unit("./ao-config-drift.service");
	const timer = await unit("./ao-config-drift.timer");

	// Oneshot runner: run the check against the deployed source tree, exit;
	// surfaced via status/journal. Pin the exact ExecStart path so a wrong
	// deployment root is caught.
	assert.match(service, /^Type=oneshot$/m);
	assert.match(
		service,
		/^ExecStart=\/usr\/bin\/env node %h\/\.ao\/deploy\/current\/source\/ops\/config-drift-check\.mjs$/m,
	);
	assert.match(service, /^Environment=AO_BIN=%h\/\.local\/bin\/ao$/m);
	// Drift is surfaced, never self-healed: the unit must not invoke a refresh
	// or an apply, and a oneshot must carry NO Restart= directive (no storm).
	assert.doesNotMatch(service, /--refresh/);
	assert.doesNotMatch(service, /config apply/);
	assert.doesNotMatch(service, /^Restart=/m);

	// Timer drives it on a conservative, pinned cadence, mirroring ao-tmux-claim.
	assert.match(timer, /^OnBootSec=5min$/m);
	assert.match(timer, /^OnUnitInactiveSec=1h$/m);
	assert.match(timer, /^Unit=ao-config-drift\.service$/m);
	assert.match(timer, /^\[Install\]$/m);
	assert.match(timer, /^WantedBy=timers\.target$/m);
});

test("the units' tmux socket path matches AO_DATA_DIR + the adapter's layout", async () => {
	const ao = await unit("./ao.service");
	const aoTmux = await unit("./ao-tmux.service");

	// backend/internal/adapters/runtime/tmux.SocketPath: <dataDir>/run/tmux/default.
	const dataDir = ao.match(/^Environment=AO_DATA_DIR=(.+)$/m)?.[1];
	assert.ok(dataDir, "ao.service must pin AO_DATA_DIR");
	const socket = `${dataDir}/run/tmux/default`;

	// Two copies of one fact live across the Go/systemd boundary; this is the
	// check that keeps them agreeing.
	assert.ok(aoTmux.includes(`tmux -S ${socket} start-server`), `ao-tmux.service must start its server on ${socket}`);
	assert.ok(ao.includes(`tmux -S ${socket} list-sessions`), `ao.service must gate on ${socket}`);
});
