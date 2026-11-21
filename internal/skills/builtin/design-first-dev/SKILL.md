---
name: "design-first-dev"
description: "Use for any feature or dev task: design diagrams first, wait for approval, build skeleton-first with KISS, then write tests, docs and a security check."
---

# Design-First Development Workflow

Follow these phases in order. Do not skip a phase. Do not merge phases. The two **STOP** gates are hard stops: do not continue until the user says to.

---

## Core rules (apply in every phase)

1. **Build exactly what was asked, nothing more.** No extra features, no "while I'm here" refactors, no speculative abstractions, no future-proofing the user didn't ask for. If you think something extra is needed, list it under "Suggestions (not built)" and move on.
2. **KISS.** Choose the simplest solution that works. Prefer plain functions over classes, standard library over new dependencies, and readable code over clever code. Only add a dependency if it's clearly justified, and say why.
3. **No orphan code.** Never write code that nothing can reach yet. Build the access point first, then the logic behind it (see Phase 4).
4. **Stay in scope.** Only touch files the current feature needs. If an unrelated bug turns up, report it; don't fix it unless asked.

---

## Phase 1 — Understand and scope

1. Read the relevant parts of the existing codebase (structure, conventions, stack, existing models and routes) before proposing anything.
2. Restate the feature in 3–6 bullets: what it does, inputs, outputs, who uses it.
3. Write an explicit **In scope** and **Out of scope** list.
4. List assumptions and open questions. If an open question changes the design, ask it now instead of guessing.

## Phase 2 — Design artifacts (required before any code)

Create all three diagrams in **Mermaid** and save them to `docs/design/<feature-name>.md`:

1. **State machine diagram** (`stateDiagram-v2`): every state the main entity or flow can be in, the transitions between states, the events/triggers, and error/failure states.
2. **ERD** (`erDiagram`): the entities/tables this feature adds or changes, their key fields with types, primary/foreign keys and relationships with cardinality. Mark which tables are new and which are modified.
3. **Data flow diagram** (`flowchart`): how data moves from the user/client through API → services → database/external systems and back. Include where validation, auth and error handling happen.

Also include in the same file:
- **Skeleton plan**: the list of access points (endpoints, routes, CLI commands, UI screens) with method, path, request and response shape, written as a table.
- **Build order**: the numbered order you will build in, skeleton first.
- **Test plan**: which unit and integration tests you will write.

Show the user all three diagrams in your reply, not just a file path.

### STOP: Gate 1 — design approval
End with: *"Please review the state machine, ERD and data flow. Reply 'approved' or tell me what to change."*
**Write no implementation code until the user approves.** If they request changes, update the diagrams and ask again.

## Phase 3 — Branch and set up

- Create a feature branch (`feature/<feature-name>`) if the project uses git.
- Add any approved migrations/schema changes matching the approved ERD exactly.

## Phase 4 — Implement skeleton-first

**Rule: Access point first, then logic.** Never write logic that has no way to be called.

1. **Skeleton:** create every access point from the approved skeleton plan with the correct signatures, request/response schemas and stubbed handlers (e.g. return `501 Not Implemented` or a fixed placeholder response). Make sure the app runs and the routes are reachable.
   - Example: for login, first create `POST /auth/login` with its request/response models returning 501. Only then write the password check, token creation and so on that the endpoint calls.
2. **Fill in** each stub one at a time in the approved build order: validation → service/business logic → data access. Write only what that access point needs right now.
3. Follow the approved state machine. Every transition in code must exist in the diagram; if you need a new one, stop and flag it as a design change.
4. Keep functions small and single-purpose. Use the project's existing patterns and naming.
5. Commit in small, logical steps with clear messages.

If you find the design is wrong or incomplete during implementation, **stop and tell the user** with a proposed diagram update. Don't silently change the design.

## Phase 5 — Automated tests (always)

When the code is written, always add:

- **Unit tests** for every new function/service: happy path, edge cases, invalid input and error paths.
- **Integration tests** whenever the feature crosses a boundary: API endpoint → DB, external services, auth flow. Test real request/response behaviour through the access points. Use a test DB or fixtures, never production data.
- **State machine coverage:** test each valid transition and at least one invalid transition being rejected.

Run the full test suite and the linter/formatter. Fix failures before moving on. Report the results (passed/failed counts, plus coverage if available).

## Phase 6 — Documentation

Write docs in `docs/` (Markdown by default; produce a PDF instead if the user asks or the project already uses PDFs):

- `docs/<feature-name>.md`: purpose, how it works, link to the design diagrams, endpoints/usage with example requests and responses, configuration and env vars, error codes, and how to run its tests.
- Update the README and changelog if the project has them.
- Add docstrings/comments only where the code isn't obvious.

## Phase 7 — Security check (always)

Review **only the code just written** and report findings in a table: issue, severity, file:line, fix.

Checklist:
- **Input validation:** all external input validated and typed; size limits set.
- **Injection:** parameterised queries only; no string-built SQL, shell or eval.
- **AuthN/AuthZ:** every new endpoint has the right authentication; users can only access their own resources (check for IDOR); role checks are server-side.
- **Secrets:** no hard-coded keys, tokens or passwords; secrets come from env/config; nothing sensitive in logs or error messages.
- **Passwords & tokens:** strong hashing (bcrypt/argon2); token expiry; secure cookie flags where relevant.
- **Data exposure:** responses return only needed fields; no stack traces leaked to clients.
- **Rate limiting / abuse:** on login, signup, OTP and other sensitive endpoints.
- **CORS, headers, HTTPS** configuration if touched.
- **Dependencies:** run the stack's audit tool (e.g. `pip-audit`, `npm audit`) and a static scanner if available (e.g. `bandit`, `semgrep`).
- **Privacy:** personal data handled in line with applicable law (e.g. POPIA/GDPR): minimal collection, no PII in logs.

Fix any **high/critical** issues right away (they're inside the scope of the feature), re-run the tests, then report what was fixed and what remains.

## Phase 8 — Final review and handoff

### STOP: Gate 2 — final review
Give the user a short summary:
- What was built (mapped to the in-scope list)
- Files changed
- Test results
- Docs location
- Security findings and fixes
- **Suggestions (not built)**: anything you noticed but deliberately didn't build

Do not merge, push or deploy unless the user explicitly says to.

---

## Definition of done
- [ ] Diagrams approved by the user
- [ ] Only in-scope code written; skeleton built before logic
- [ ] Unit tests and relevant integration tests passing
- [ ] Lint/format clean
- [ ] Docs written in `docs/` (or PDF)
- [ ] Security check done, high/critical issues fixed
- [ ] Summary delivered to the user