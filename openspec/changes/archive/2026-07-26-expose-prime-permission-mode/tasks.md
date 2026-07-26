## 1. Spec and CLI

- [x] 1.1 Validate the OpenSpec delta for Prime permission mode.
- [x] 1.2 Add focused CLI tests for `ao prime enable --permission` and `ao prime set --permission`.
- [x] 1.3 Implement the Prime CLI permission flag using the existing permission vocabulary and persisted field.

## 2. Global Settings

- [x] 2.1 Add a focused frontend test that Prime Settings saves permission mode through `agentConfig.permissions`.
- [x] 2.2 Add the Prime permission control in global Settings, consistent with project permission settings.

## 3. Verification

- [x] 3.1 Run OpenSpec validation.
- [x] 3.2 Run focused backend CLI and frontend tests.
- [x] 3.3 Run the repo local CI gate before push.
