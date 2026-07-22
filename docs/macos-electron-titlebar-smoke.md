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

It runs on the hosted `blacksmith-6vcpu-macos-15` runner and is
`workflow_dispatch`-only. It does not run on pull requests and has only
`contents: read` permission.

## Run it

1. Open GitHub Actions for this repository.
2. Select **macOS Electron titlebar smoke**.
3. Select **Run workflow**.
4. Enter a branch or full commit SHA in `ref`.
5. Download the `electron-titlebar-smoke-*` artifact when the job finishes.

Use the full head SHA when verifying a merge-ready PR so the result is pinned to
the exact reviewed commit.

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
