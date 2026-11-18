# Domains and flavors

[Modes](modes.md) say *how* ASG works. **Domains** say *what kind of work* it
is doing, and each domain has **flavors** (sub-modes). The flavor is what
matters most: it gives the model

- **the loop it must follow**: the phases, in order;
- **hard rules** it may not break;
- **review lenses**: the angles it re-checks its own answer from
  ([self-review](review-and-questions.md));
- **final steps** for [loop runs](loop.md), such as a security test or a report.

The prompt shows the pair as `domain/flavor`, coloured per domain:

```
 BUILD  software/work  qwen3.5:9b ▸        software is lime
 BUILD  docs/update  qwen3.5:9b ▸          docs is cyan
 BUILD  cyber/red  qwen3.5:9b ▸            cyber is red
 BUILD  devops/review  qwen3.5:9b ▸        devops is amber
 BUILD  pm/sprint  qwen3.5:9b ▸            pm is magenta
```

## All domains and flavors

The first flavor listed is the domain's default.

| Domain | Flavor | What it does |
|---|---|---|
| **software** | `project` | balanced: solid, tested code for your own or a team project |
| | `work` | strict: only code this build needs, caller first, test → document → verify → security |
| | `hobby` | light: get it working fast, learn as you go |
| **docs** | `update` | screenshot the docs, see what's wrong, fix, re-screenshot |
| | `write` | write new documentation from the code up |
| **cyber** | `red` | offensive: recon → enumerate → find and validate vulnerabilities → report |
| | `blue` | defensive: inventory → threat model → detect → harden → respond → report |
| **devops** | `review` | audit pipelines: correctness, security, speed, reliability |
| | `pipeline` | write or fix pipelines and validate them |
| **pm** | `plan` | project plan: scope, work breakdown, milestones, estimates, risks |
| | `sprint` | sprint planning: backlog, capacity, sprint goal, tickets |
| | `status` | status report: progress, risks, decisions needed |

## Switching domains and flavors

Your choice is **saved as the default**, so the next `asg` starts where you left
off. The mode doesn't change when you switch domain.

### Switch flavor within a domain: software/project → software/work

Type the domain command with the flavor name:

```text
 BUILD  software/project  qwen3.5:9b ▸ /software work
  software/work · strict: only code this build needs, caller first, test → document → verify → security
 BUILD  software/work  qwen3.5:9b ▸
```

More examples:

```text
/software hobby          software/work    → software/hobby
/software project        software/hobby   → software/project
/cyber blue              cyber/red        → cyber/blue
/devops pipeline         devops/review    → devops/pipeline
/pm status               pm/plan          → pm/status
/docs write              docs/update      → docs/write
```

Flavor names are case-insensitive (`/software WORK` works).

### Switch domain: software → cyber

```text
/cyber                   → cyber/red  (the domain's default flavor)
/cyber blue              → cyber/blue
/docs                    → docs/update
/pm sprint               → pm/sprint
```

When you switch to a **different** domain without naming a flavor, you get that
domain's default flavor. When you type the domain you are **already** in without
a flavor (`/software` while in software/work), the flavor stays and ASG just
prints the current track.

### Switch and send in one go

Anything after the flavor is sent as your request, in the new domain:

```text
/software work add a --json flag to the list command
/cyber blue audit the nginx config in ./deploy and harden it
/devops review check .github/workflows for security problems
/pm sprint plan the next two weeks from issues labelled v0.3
/docs update fix how the install page renders on GitHub
```

If the first word isn't a flavor name, it's treated as part of the request and
the flavor doesn't change:

```text
/software fix the failing test in parser_test.go
         └── "fix" isn't a flavor, so it stays software/<current> and sends the whole line
```

### Ctrl+T: cycle domains

**Ctrl+T** moves to the next domain:
software → docs → cyber → devops → pm → software. You land on that domain's
default flavor. Like Shift+Tab, it redraws the status line in place, and
whatever you had typed stays on the line.

```
   build  plan  loop  ask  │ software/work · strict: only code this build needs, …
            ⌃ Ctrl+T
   build  plan  loop  ask  │ docs/update · screenshot the docs, see what's wrong, fix, re-screenshot
            ⌃ Ctrl+T
   build  plan  loop  ask  │ cyber/red · offensive: recon → enumerate → find and validate …
```

Ctrl+T then `/cyber blue` gets you to cyber/blue. Ctrl+T is fastest for
changing domain; a `/domain` command is fastest for picking a flavor.

### `/domain`: menu

```text
/domain
```

opens a two-step menu. First pick the domain, then the flavor (the current one
is marked):

```
  domain
  ▸ software   write, fix and ship code
    docs       write and improve documentation, looking at how it renders
    cyber      authorized red team (attack) or blue team (defend) work, ending in a full report
    devops     review and fix CI/CD pipelines, builds, containers and infrastructure
    pm         project management: plans, sprints, risks, status — reviewed at least 4 times

  software · flavor
    project    balanced: solid, tested code for your own or a team project
  ▸ work       strict: only code this build needs, caller first, test → document → verify → security
    hobby      light: get it working fast, learn as you go
```

`/domain` also takes arguments, which is handy in scripts or when you remember
the name:

```text
/domain cyber blue
/domain devops review check the release workflow
```

Domain aliases accepted by `/domain` and `asg -domain`:

| Domain | Also accepts |
|---|---|
| software | `sw`, `code`, `dev` |
| docs | `doc`, `documentation` |
| cyber | `security`, `sec` |
| devops | `ops`, `ci`, `cicd` |
| pm | `project-manager`, `projectmanager`, `manager` |

### From the command line

```sh
asg -domain software/work
asg -domain cyber/blue
asg -domain devops                  # default flavor (review)
asg -domain pm/sprint -mode plan
asg -domain software/work -loop -p "add a --json flag to list, with tests"
```

---

## software

### software/project (default)

The everyday flavor for your own or a team codebase.

1. **Plan**: understand the request and the code it touches.
2. **Build**: make the change in small steps, following the project's patterns.
3. **Test**: add or update tests and run them.
4. **Document**: update the README or docs the change affects.
5. **Verify**: build and run the tests, fixing failures until they pass.

Rules: keep changes focused (no unrelated refactors) and prefer simple code.

### software/work

For code that goes to production. These rules are strict and the loop's step
reviewer **fails** any step that breaks them.

1. **Plan**: restate exactly what is being built and list the pieces it needs.
   Nothing else goes in.
2. **Caller first**: write the code that will *call* a new function before the
   function itself, so the call site decides its shape.
3. **Implement**: write the function exactly as the caller uses it.
4. **Test**: write or extend tests and run them.
5. **Document**: update the docs, comments and changelog this change affects.
6. **Verify**: run the build and the full test suite. On failure, read it,
   find the root cause, fix, verify again, and loop until everything passes.
7. **Security**: check for injection, unsafe input, secrets, path traversal,
   permissions and risky dependencies, then fix and verify again.

Hard rules:

- Write **only** code that this exact build definitely uses.
- **No anticipation code**: no speculative parameters, options, hooks,
  interfaces, abstractions, config or "for later" helpers.
- Work top-down: caller before function body.
- A failing test is never skipped or deleted to get green.

Self-review lenses: anticipation code, caller-first wiring, tests and
verification, security. In a loop run a **security test** of every changed file
is added as a final step.

```text
/software work add retry with backoff to the HTTP client, used by the sync command
```

### software/hobby

1. **Sketch**: decide the quickest path to something that works.
2. **Build**: make it work end to end.
3. **Try it**: run it once and fix what breaks.

It favours getting something working over polish, and explains new things you
might want to learn.

---

## docs

Docs mode works from **what the docs look like**, not just their source. It uses
the [`screenshot`](tools.md#screenshot) tool, so you need a model that can read
images.

### docs/update (default)

Screenshot the rendered page → decide the changes from the image → edit →
screenshot again and compare. It repeats until the page renders right. A loop
run ends with a last screenshot pass.

```text
/docs update the tables in docs/commands.md look broken, fix them
/docs update check http://localhost:3000/docs/install and fix layout problems
```

### docs/write

Writes new documentation from the code up, then screenshots it to check how it
renders.

```text
/docs write a getting-started page for the CLI in docs/
```

---

## cyber

For **authorized** security work only. Before any active testing it confirms
the scope and your authorization with you (through
[`ask_user`](review-and-questions.md#ask_user)). Both flavors end with a
`report.md` in the [work folder](work-folder.md) covering **everything it did
and found**.

### cyber/red (default): attack

scope → recon → enumerate → analyse → validate findings → report

```text
/cyber red test the login form of my local app at http://localhost:8080 for injection
```

### cyber/blue: defend

inventory → threat model → detect → harden → respond → report

```text
/cyber blue audit ./deploy/nginx.conf and the Dockerfile, then harden them
```

---

## devops

### devops/review (default)

Inventories every pipeline and infrastructure file, then reviews each for
**correctness**, **security** (permissions, action pinning, secrets, script
injection), **speed** and **reliability**. It ends with a findings report giving
exact fixes.

```text
/devops review .github/workflows
/ask
/devops review why is our release job slow?
```

### devops/pipeline

Writes or fixes pipelines and validates them.

```text
/devops pipeline add a workflow that runs go test on Windows, macOS and Linux
```

---

## pm

Project-manager work: scope → breakdown → sequence → estimate → risks →
communicate. It bases plans on what's actually in the repo, issues and notes,
and asks you for deadlines, team size and priorities it can't find. The four
[self-review](review-and-questions.md) passes use **planning angles**:
dependencies and order, estimates and capacity, risks and assumptions, scope and
acceptance criteria.

### pm/plan (default)

```text
/pm plan a project plan for moving auth to OAuth, with milestones and risks
```

### pm/sprint

```text
/pm sprint plan a two-week sprint from TODO.md for two developers
```

### pm/status

```text
/pm status write this week's status report from the git log and open issues
```

PM pairs well with **plan** mode (it never touches files) or **build** mode
(it can write the plan to a file, or to a `.docx` / `.xlsx` with
[`office_document`](tools.md#office_document)).

Next: [Loop harness](loop.md)
