# Self-review and questions

## Self-review

After **every** answer, in every mode and domain, ASG checks its own work
again, **at least 4 times**. Each pass looks from a different angle (a "lens").
The flavor's own lenses come first, then the general ones:

| General lenses | Asks |
|---|---|
| logic | does each step follow from the facts and tool results? |
| completeness | does the answer cover the request line by line? |
| edge cases | empty input, errors, missing files: what breaks? |
| evidence | is every claim backed by something it actually read or ran? |

Some flavors bring their own lenses, for example:

| Flavor | Its lenses |
|---|---|
| software/work | anticipation code · caller-first and wiring · tests and verification · security |
| software/project | scope · tests |
| cyber/red | scope · evidence · severity · remediation |
| cyber/blue | coverage · evidence · priority · regressions |
| devops/* | security · correctness |
| pm/* | dependencies and order · estimates and capacity · risks and assumptions · scope and acceptance |

### What you see

One line per pass:

```
  ◇ review 1/4 anticipation code             ✓ nothing unused
  ◇ review 2/4 caller-first and wiring       ✓ flag is read in main
  ◇ review 3/4 tests and verification        ↻ re-ran go test, fixed a failing case
  ◇ review 4/4 security                      ✓ no new input paths
```

- `✓`: the pass found nothing.
- `↻`: the pass found a problem and **fixed it**, using tools if needed.
  The corrected answer is printed after the reviews and ends with
  "(Revised during self-review.)".
- If the **last** required pass still found something, extra passes run
  (up to `max_passes`) until one comes back clean.

The review messages are folded out of the conversation afterwards, so they don't
fill up the model's context.

### In loop runs

Loops are reviewed at two more points:

- the **plan** is reviewed 4 times before the first step runs;
- the **finished result** is reviewed 4 times at the end. If that changes
  anything, verification runs again.

### Changing it

Each pass costs a model call. With the default of 4, every answer takes at
least 5 calls. On a slow local model you may want fewer.

```text
/review              show the current setting
/review 2            at least 2 passes (1–12)
/review 6            at least 6 passes
/review off          no self-review
```

```sh
asg -review 2        # set it from the command line (saved)
asg -review -1       # off
```

```json
"review": { "passes": 4, "max_passes": 6 }
```

`max_passes` defaults to `passes + 2`.

## ask_user

When the model needs information it can't find in the project (a requirement,
a credential, the scope of a test, a decision only you can make), it can
**pause at any moment** and ask you:

```
  ╭─  ? ASG NEEDS YOUR INPUT  ────────────────────────────────────
  │ Which port should the dev server use? 3000 is taken by another process.
  │   1 3001
  │   2 8080
  │   3 stop the other process
  ╰────────────────────────────────────────────────
  1–3 or type your own ▸ 2
```

- Type a **number** to pick a suggestion, or type **your own answer**.
- **Ctrl+C** cancels the question and stops the turn.
- The model carries on with your answer in the same turn; nothing is restarted.

It works in **every mode**, including plan and ask (asking changes nothing), loop
runs, and `asg -p` when run in a terminal. Cyber always uses it to confirm scope
and authorization before active testing. PM uses it for deadlines, team size and
priorities.

When there's nobody to ask (the browser dashboard, or `asg -p` with input piped
in), the model is told so. It then makes a reasonable assumption and **states
it** in the answer.

### Loop questions

Inside a loop, a step that can't continue without you **stops the loop** and
shows the question. Answer with:

```text
/loop resume use port 8080
```

Next: [Work folder](work-folder.md)
