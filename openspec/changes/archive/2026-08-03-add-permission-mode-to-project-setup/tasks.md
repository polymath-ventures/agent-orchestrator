## 1. Contract and Regression Tests

- [x] 1.1 Validate the OpenSpec change for project setup permission mode.
- [x] 1.2 Add a failing setup-sheet test that selects bypass permissions and emits it in the create selection.
- [x] 1.3 Add failing project-config tests for explicit permission persistence, composition with model defaults, and untouched-default omission.

## 2. Shared Permission Vocabulary

- [x] 2.1 Extract the existing explicit permission-mode options into a shared frontend module.
- [x] 2.2 Update project settings and Prime to consume the shared options without changing their saved-value or default-label behavior.

## 3. First-Run Setup

- [x] 3.1 Add the compact permission-mode control to the shared project setup sheet and reset it with the rest of the form.
- [x] 3.2 Carry the optional selection through project creation and persist it in `ProjectConfig.agentConfig.permissions` without dropping model defaults.

## 4. Verification

- [x] 4.1 Run the focused frontend tests and typecheck.
- [x] 4.2 Exercise the web-mode setup flow and confirm the selected mode reaches the create-project request.
- [x] 4.3 Run the required local CI-parity gate before push.
