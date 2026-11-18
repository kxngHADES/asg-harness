# Work folder

In **build** mode and **loop** runs, ASG keeps a folder of documents you can read
*while it works*. You can follow along, see why it made a choice, and pick up
later.

```
your-project/
└── .asg/work/
    └── 2026-11-18-add-a-json-flag-to-the/
        ├── plan.md          goal, approach, steps (written first)
        ├── checklist.md     - [ ] / - [x] per step, ticked as it goes
        ├── walkthrough.md   what changed, where, why, how to check it
        ├── notes.md         decisions, assumptions, open questions
        ├── report.md        cyber and devops: everything done and found
        └── activity.md      every tool call, written by the harness itself
```

The folder name is the date plus the first words of your request.

| File | Written by | Contents |
|---|---|---|
| `plan.md` | model (in a loop: the harness) | goal, approach, numbered steps |
| `checklist.md` | model (in a loop: the harness) | one checkbox per step, ticked off as it goes |
| `walkthrough.md` | model (in a loop: the harness) | what changed, in which files, why, and how to verify it, plus a final review section |
| `notes.md` | model | decisions, assumptions, things to ask you |
| `report.md` | model | cyber and devops/review: the full report |
| `activity.md` | **harness** | time, tool, summary and result of every call; complete even if the model forgets to write anything |

## Using it

```text
/work
```

shows the folder, its files with sizes and times, and the checklist:

```
  WORK  .asg/work/2026-11-18-add-a-json-flag-to-the
    activity.md           2210 bytes · 14:02:31
    checklist.md           188 bytes · 14:02:10
    plan.md                612 bytes · 14:01:58
    walkthrough.md         940 bytes · 14:02:29

- [x] call the JSON printer from list when --json is set
- [x] implement the JSON printer
- [ ] tests for --json
```

After each reply ASG also prints a one-line hint pointing at the folder. Open
the files in your editor while it runs; they're plain Markdown.

## Rules

- Writes **inside** the work folder never ask for permission, so the model
  doesn't flood you with gates for its own notes. Everything outside it still
  goes through the normal [gates](permissions.md).
- The folder is created **lazily**, on the first write. A chat turn that changes
  nothing leaves nothing behind.
- Plan and ask modes don't create one (they're read-only).
- `/clear` starts a new conversation and, with it, a new work folder on the next
  build request.
- A loop keeps one folder for the whole run, including after `/loop resume`.

## Git

Add the folder to `.gitignore` if you don't want to commit it:

```gitignore
.asg/work/
```

Or commit it on purpose: a `walkthrough.md` or `report.md` is a good thing to
attach to a pull request.

Next: [Tools](tools.md)
