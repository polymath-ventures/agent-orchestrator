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
  assert.match(text, /^ExecStartPre=.*tmux list-sessions/m);
  assert.match(text, /^Environment=AO_DATA_DIR=%h\/\.ao$/m);
});

test("ao-tmux.service owns the default tmux socket and refuses stop-job fleet kills", async () => {
  const text = await unit("./ao-tmux.service");

  assert.match(text, /^Before=ao\.service$/m);
  assert.match(text, /^RefuseManualStop=yes$/m);
  assert.match(text, /^Type=forking$/m);
  assert.match(text, /^ExecStart=.*tmux start-server.*exit-empty off/m);
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
