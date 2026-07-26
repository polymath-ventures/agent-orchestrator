## 1. Derivation rule in the domain

- [x] 1.1 Write failing tests in `backend/internal/domain/session_prefix_test.go` for the derivation rule: multi-word name yields initials (`Coach Claw` → `cc`, `Polymath Ventures Inc` → `pvi`), single-word name yields leading characters (`mirrorborn` → `mir`), the result is capped at 3 characters, and the same inputs yield the same output twice.
- [x] 1.2 Add failing collision tests: a taken candidate lengthens from the name's own characters (`cc` taken → `coa`, the leading run of the concatenated words), an exhausted name falls back to the smallest free numeric suffix within the cap (`cc` and every lengthening taken → `cc2`), and comparison against taken prefixes is case-insensitive.
- [x] 1.3 Add failing tests for the unusable-name path: a name with no alphanumeric characters derives from the project id, an empty name and empty id still yield a non-empty 3-character prefix, and two different ids on that path do not yield the same prefix.
- [x] 1.4 Implement `DeriveSessionPrefix` in `backend/internal/domain/session_prefix.go` — inputs are the project name, the project id, and the set of prefixes already in use; output is a lowercase alphanumeric prefix of at most 3 characters. Make 1.1–1.3 pass.
- [x] 1.5 Verify the output satisfies `NameRuneAllowed` and `validateNameComponent` for every case covered, so a derived prefix can never be rejected by config validation.

## 2. Wire the create path

- [x] 2.1 Write a failing test in `backend/internal/service/project` asserting that `Add` with a name and no session prefix persists a derived prefix, and that `Add` with an explicit prefix persists it unchanged.
- [x] 2.2 Write a failing test asserting the collision case end to end: register a project whose resolved prefix is already taken by another project (including a project with no stored prefix whose _resolved_ prefix comes from a short id, e.g. id `ao`) and assert the new project gets a distinct prefix.
- [x] 2.3 Write a failing test asserting `Add` still succeeds and persists a non-empty prefix when the project name yields no usable characters.
- [x] 2.4 In `Service.Add`, list projects once and use the result for both the existing `projectCountBefore` check and the taken-prefix set; fill `row.Config.SessionPrefix` from `DeriveSessionPrefix` only when it is blank, before the workspace / single-repo persist branch so both paths are covered. Make 2.1–2.3 pass.
- [x] 2.5 Route `EnsureDefaultScratchProject` through the same derivation, with a test asserting the seeded scratch project carries a derived prefix.

## 3. Confirm the boundaries hold

- [x] 3.1 Add a test asserting an existing project with no stored prefix still resolves through the legacy `sessionPrefix(id)` fallback unchanged — nothing renames itself and no derivation runs outside creation.
- [x] 3.2 Confirm `UpdateSettings` / `SetConfig` still accept an operator-supplied prefix verbatim, with no derivation and no collision refusal.
- [x] 3.3 Confirm the frontend settings form needs no functional change: verify the create response carries the derived prefix so the field renders populated (already exercised by `ProjectSettingsForm.test.tsx`, which round-trips a populated `sessionPrefix`), and drop the misleading `ao` placeholder that named one project's prefix as if it were every project's default.

## 4. Gate

- [x] 4.1 Run `npm run ci-local` from the repo root and fix anything it reports.
- [x] 4.2 Run `openspec validate --changes derive-session-prefix --strict` and fix any spec violations.
