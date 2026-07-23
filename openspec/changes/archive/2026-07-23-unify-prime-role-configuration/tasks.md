## 1. Spec and Backend Contract

- [x] 1.1 Add or update backend tests for Prime wake interval bounds and
      removal of legacy Prime environment/project reporting from service, API,
      and CLI surfaces.
- [x] 1.2 Remove legacy Prime environment/project reporting from config/service
      DTOs, CLI output, OpenAPI, and generated TypeScript schema.
- [x] 1.3 Enforce Prime wake interval lower and upper bounds while preserving
      the existing duration-string storage shape.

## 2. Shared Frontend Controls

- [x] 2.1 Add focused tests for Prime using Harness terminology, shared
      model/effort picker behavior, custom model fallback warning, and wake
      interval minute conversion.
- [x] 2.2 Reuse or extract shared harness/model/effort controls for Prime
      without duplicating model catalog logic.
- [x] 2.3 Update Prime instructions/rules labels and helper text, including
      inline-first then file-appended assembly order.
- [x] 2.4 Clarify project role and fleet Prime instructions file path labels
      and helper text.

## 3. Verification

- [x] 3.1 Run focused frontend and backend tests for Prime settings, model
      selector reuse, wake interval validation, and legacy removal.
- [x] 3.2 Run `npm run api`, `npm run frontend:typecheck`, relevant backend Go
      tests, and OpenSpec validation.
