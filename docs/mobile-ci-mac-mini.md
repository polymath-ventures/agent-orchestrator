# Mobile CI and TestFlight: Mac Mini Runner

This document describes how the AO iOS app in `packages/mobile` is built and
uploaded to TestFlight. The `Mobile` workflow at
`.github/workflows/mobile.yml` handles CI. Linux verification runs on GitHub's
hosted Ubuntu runner, while the iOS archive and TestFlight upload run on a
self-hosted Mac mini registered to `polymath-ventures/agent-orchestrator`.

No Apple credentials, signing identities, provisioning profiles, or API keys
belong in the repo. They live in GitHub Actions secrets and the Mac keychain.

## What Runs Where

| Job              | Runner                              | Triggers                                                | Purpose                                                                |
| ---------------- | ----------------------------------- | ------------------------------------------------------- | ---------------------------------------------------------------------- |
| `verify`         | `ubuntu-latest`                     | PRs and `main` pushes touching `packages/mobile/**`     | `npm ci`, `npm run typecheck`, `npm test`, informational `expo-doctor` |
| `ios-testflight` | `[self-hosted, macOS, ao-mac-mini]` | `main` pushes touching mobile paths and manual dispatch | Expo prebuild, pods, Fastlane archive, optional TestFlight upload      |

The Mac job never runs on pull requests. A single self-hosted Mac should not
gate every PR; the Linux `verify` job is the PR gate.

## Prerequisites

1. A Mac mini on your Tailnet, with a build user that can stay awake for CI.
2. Apple Developer Program access for the team that owns bundle identifier
   `aoagents.ao`.
3. Admin access to the GitHub repo so you can register a self-hosted runner and
   add Actions secrets.
4. The existing App Store Connect app record for `aoagents.ao`; keep this
   bundle identifier so TestFlight continuity is preserved.

## Mac Setup

Install Tailscale and enable SSH:

```bash
brew install --cask tailscale
```

Then enable Remote Login in System Settings and verify access from another
Tailnet device:

```bash
ssh <user>@<mac-mini-tailscale-name>
```

Keep the machine awake:

```bash
sudo pmset -a sleep 0 disablesleep 1
```

Install Xcode and build tooling:

```bash
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -license accept
xcodebuild -version

brew install node@20 cocoapods
sudo gem install bundler
```

The workflow uses `actions/setup-node`, but a local Node 20 installation keeps
manual fallback builds aligned with CI.

## Signing Setup

Fastlane uses automatic signing with `-allowProvisioningUpdates`, so
provisioning profiles are fetched during the archive and are not committed.

1. Open Xcode on the Mac.
2. Sign in to the Apple Developer account for the AO team.
3. Create or import an Apple Distribution certificate into the login keychain.
4. Confirm the identity exists:

```bash
security find-identity -v -p codesigning
```

The output should include an `Apple Distribution` identity for the team.

## App Store Connect API Key

Create an App Store Connect API key:

1. Go to App Store Connect, then Users and Access, then Integrations.
2. Create a Team Key with the App Manager role.
3. Download the `.p8` file once and store it outside the repo.
4. Note the Key ID and Issuer ID.

## Register The Runner

In GitHub, open this repo's Settings, then Actions, then Runners, then New
self-hosted runner, and choose macOS. Run the generated commands on the Mac.
During `./config.sh`, use this repo URL and labels:

```bash
./config.sh --url https://github.com/polymath-ventures/agent-orchestrator \
  --token <REGISTRATION_TOKEN> \
  --labels self-hosted,macOS,ao-mac-mini \
  --name ao-mac-mini
```

Install and start it as a service:

```bash
./svc.sh install
./svc.sh start
```

The `ao-mac-mini` label must match the workflow exactly.

## Configure Secrets

Add these repository Actions secrets in GitHub:

| Secret           | Value                                         |
| ---------------- | --------------------------------------------- |
| `ASC_KEY_ID`     | App Store Connect API Key ID                  |
| `ASC_ISSUER_ID`  | App Store Connect Issuer ID                   |
| `ASC_API_KEY_P8` | Full PEM contents of the downloaded `.p8` key |
| `APPLE_TEAM_ID`  | 10-character Apple Developer Team ID          |

The workflow preflight step prints the exact missing names if any are absent.
It fails before prebuild or archive work starts, so a misconfigured repo does
not produce a confusing half-signed build.

Fastlane queries App Store Connect for the latest TestFlight build number and
writes the next `CFBundleVersion` into the generated `ios/AO/Info.plist` before
archiving. This keeps the Mac mini path compatible with any earlier EAS or
manual TestFlight uploads for the same app version.

## Run A Build

Automatic TestFlight upload happens when a change under `packages/mobile/**` or
`.github/workflows/mobile.yml` lands on `main`.

Manual runs are available from Actions, then Mobile, then Run workflow. The
`upload_testflight` input defaults to true. Turn it off for a build-only dry
run that still archives the app and uploads artifacts.

For a local fallback on the Mac:

```bash
cd packages/mobile
npm ci
npx expo prebuild --platform ios --no-install
npx pod-install ios
bundle install

export ASC_KEY_ID=... ASC_ISSUER_ID=... APPLE_TEAM_ID=...
export ASC_API_KEY_P8="$(cat /path/to/AuthKey_XXXX.p8)"
export UPLOAD_TESTFLIGHT=true
bundle exec fastlane ios beta
```

The signed `.ipa` is written to `packages/mobile/build/AO.ipa`.

## Troubleshooting

| Symptom                                      | Fix                                                                                                                            |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Job waits for a runner                       | On the Mac, run `./svc.sh status` and `./svc.sh start`. Confirm the runner labels are `self-hosted,macOS,ao-mac-mini`.         |
| Preflight lists missing secrets              | Add the named secrets in repo Settings, then Actions, then Secrets and variables.                                              |
| Xcode cannot find a distribution certificate | Create or import an Apple Distribution certificate in the login keychain and rerun `security find-identity -v -p codesigning`. |
| App Store Connect auth fails                 | Check `ASC_KEY_ID`, `ASC_ISSUER_ID`, and the complete `ASC_API_KEY_P8` PEM text.                                               |
| Provisioning fails                           | Confirm the API key has App Manager access and the `aoagents.ao` bundle identifier belongs to the selected team.               |
| Pods fail                                    | Install CocoaPods on the Mac with `brew install cocoapods` or `sudo gem install cocoapods`, then rerun `npx pod-install ios`.  |

## Security Notes

The package `.gitignore` excludes `.p8`, `.p12`, `.mobileprovision`, `.key`,
`.pem`, generated native folders, Fastlane artifacts, Bundler output, local
environment files, and Firebase service-account JSON. The workflow scopes Apple
secrets only to the preflight and Fastlane steps that require them.
