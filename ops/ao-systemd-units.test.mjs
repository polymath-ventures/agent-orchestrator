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
	assert.match(text, /^ExecStartPre=.*tmux -S "\$AO_DATA_DIR\/run\/tmux\/default" list-sessions/m);
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
	assert.match(text, /^ExecStart=.*tmux -S \$\{AO_DATA_DIR\}\/run\/tmux\/default start-server.*exit-empty off/m);
	// tmux will not bind a socket whose directory is missing; 0700 matches
	// tmux's own /tmp/tmux-$UID.
	assert.match(text, /^ExecStartPre=.*install -d -m 700 \$\{AO_DATA_DIR\}\/run\/tmux$/m);
	assert.match(text, /^ExecCondition=.*tmux -S "\$AO_DATA_DIR\/run\/tmux\/default" list-sessions/m);
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

test("both units derive the tmux socket from one AO_DATA_DIR, never a hardcoded copy", async () => {
	const units = [
		["ao.service", await unit("./ao.service")],
		["ao-tmux.service", await unit("./ao-tmux.service")],
	];

	// One fact, one place. Hardcoding the resolved path in a unit means a
	// drop-in overriding AO_DATA_DIR moves the daemon's socket while the
	// ownership gate keeps probing the old one — the gate then protects nothing.
	// Check EVERY -S occurrence and EVERY assignment, not just the first: a
	// single derived reference alongside a hardcoded one would otherwise pass.
	const dataDirs = new Map();
	for (const [name, text] of units) {
		const sockets = [...text.matchAll(/-S\s+("[^"]*"|\S+)/g)].map((m) => m[1].replace(/"/g, ""));
		assert.ok(sockets.length > 0, `${name} must pass -S to tmux`);
		for (const socket of sockets) {
			assert.match(
				socket,
				/^(\$\{AO_DATA_DIR\}|\$AO_DATA_DIR)\/run\/tmux\/default$/,
				`${name}: socket ${socket} must derive from AO_DATA_DIR, not hardcode a path`,
			);
		}

		const assignments = [...text.matchAll(/^Environment=AO_DATA_DIR=(.+)$/gm)].map((m) => m[1]);
		assert.equal(assignments.length, 1, `${name} must pin AO_DATA_DIR exactly once (found ${assignments.length})`);
		dataDirs.set(name, assignments[0]);
	}

	// Both units must agree on the data dir itself, since ao-tmux.service owns
	// the socket ao.service's daemon then connects to. An operator overriding
	// AO_DATA_DIR must edit both units — see docs/fork.md.
	assert.equal(
		dataDirs.get("ao-tmux.service"),
		dataDirs.get("ao.service"),
		"ao-tmux.service must pin the same AO_DATA_DIR as ao.service",
	);

	// "/run/tmux/default" is the one systemd-side copy of the layout in
	// backend/internal/adapters/runtime/tmux.SocketPath(<dataDir>).
});
