# macOS Electron titlebar smoke

The manual **macOS Electron titlebar smoke** workflow verifies the renderer
chrome that only exists inside the real macOS Electron window:

- the preload bridge (`window.ao`) is present;
- the Electron-only `.titlebar-nav` cluster renders;
- Electron's native window-button position is available;
- the renderer cluster clears the measured macOS traffic-light lane with the
  intended group gap;
- renderer and best-effort whole-desktop screenshots plus `geometry.json` are
  uploaded as workflow artifacts.

It runs on the standard GitHub-hosted `macos-15` runner, which is free for this
public repository, and is `workflow_dispatch`-only. It does not run on pull
requests and has only `contents: read` permission.

## Run it

1. Open GitHub Actions for this repository.
2. Select **macOS Electron titlebar smoke**.
3. Select **Run workflow**.
4. Enter a branch or full commit SHA in `ref`.
5. Download the `electron-titlebar-smoke-*` artifact when the job finishes.

Use the full head SHA when verifying a merge-ready PR so the result is pinned to
the exact reviewed commit.

## Harness and target are separate

The workflow checks out the merged smoke harness from the repository's default
branch into `harness/` and the requested `ref` into `target/`. Packaging runs
from `target/frontend`, while Playwright runs from `harness/frontend`.

This separation is intentional: the target ref may predate this workflow and
therefore may not contain `test:electron-titlebar` or the native smoke files.
The target app is still built entirely from the requested ref; only the test
harness comes from the repository's current default branch.

Every run writes `dispatch.json` before installing or packaging. It records the
harness SHA, requested ref, and resolved target SHA, so the always-uploaded
artifact identifies exactly what was attempted even on an early failure.

The uploaded evidence directory is separate from Playwright's own output
directory. Native launch failures include `app-stdout.log`, `app-stderr.log`,
`launch-failure.json`, and the retained Playwright trace. Electron cleanup is
bounded so a failed launch cannot hold the runner until the job timeout.

## Why the app is staged in `/Applications`

Packaged macOS builds call Electron's `moveToApplicationsFolder()` at startup.
Launching the forge output in place would relaunch and detach from Playwright.
The workflow copies the unsigned, locally-built app into `/Applications` first,
so the call is a no-op and Playwright remains attached to the real app process.

No signing, notarization, publishing, or release upload occurs.

## Self-hosted ARM64 fallback

Use this only if the hosted runner cannot exercise a required macOS GUI path.
This is a public repository: do **not** attach a daily-use Mac account containing
personal data, signing keys, cloud credentials, or broad filesystem access.
Use an isolated macOS user, disposable machine, or VM with no repository
secrets.

Register the runner through repository **Settings → Actions → Runners**, give it
the custom label `ao-mac-ui`, and initially run it interactively from a logged-in
desktop session (`./run.sh`) so Electron can access the macOS WindowServer.

The fallback job label is:

```yaml
runs-on: [self-hosted, macOS, ARM64, ao-mac-ui]
```

Keep the same workflow safeguards:

- `workflow_dispatch` only — never `pull_request`;
- `permissions: contents: read`;
- no repository or environment secrets;
- explicit, reviewed commit SHA;
- shut down or unregister the runner when the smoke is complete.
