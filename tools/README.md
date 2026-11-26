# ASG tools

Tools are what the model can *do*: read a file, run a command, write a Word
document. ASG has three kinds, and this folder is home to the shared ones.

| Kind | Where it lives | Who gets it | How to add one |
|------|----------------|-------------|----------------|
| **Built-in** | `internal/tools` | everyone | core file / shell / web tools; rarely changed |
| **Bundled** | this folder (`tools/<name>/`) | everyone — compiled into `asg` | a Go package, registered in `tools.go` |
| **Script** | `~/.asg/tools/<name>/` (you), `<project>/.asg/tools/<name>/` (that project) or `<skill>/tools/<name>/` | whoever has the folder | a `tool.json` plus a Python or Go project (or any command) |

`/tools` in the terminal lists every tool and where it came from, and
reloads script tools so you can edit them without restarting.

## Bundled tools

| Tool | What it does |
|------|--------------|
| `office_document` ([office/](office)) | Writes Word `.docx`, PowerPoint `.pptx` and Excel `.xlsx` files from Markdown or structured input. Pure Go — no Office or Python needed. |
| `computer` ([computer/](computer)) | Live computer use: sees the desktop and drives the real mouse and keyboard in a screenshot → act → verify loop. Native on Windows; xdotool on Linux; cliclick + System Events on macOS. Off until enabled. See [docs/computer-use.md](../docs/computer-use.md). |
| `screenshot` ([screenshot/](screenshot)) | Renders a URL, `.html` or `.md` file in headless Chrome/Edge (or captures the screen) and shows the image to the model. Docs mode uses it to judge how documentation really looks. |

### Adding a bundled tool

1. Create `tools/<name>/<name>.go` with a function returning `*tools.Tool`
   (from `internal/tools`): a JSON-schema `Spec`, a `Run` function, and
   `NeedsApproval: true` if it changes anything or reaches the network.
   Set `RunMedia` instead of only `Run` if it returns images.
2. Add it to `Bundled()` in [tools.go](tools.go).
3. Add tests next to it and run `go test ./...`.

Keep bundled tools dependency-free (standard library only) so they work
for everyone on every platform.

## Script tools

A folder with a `tool.json` — no rebuild. `asg tool new <name> [--go]` (or
`/tool new <name> [go]` in the terminal) scaffolds a Python or Go tool in
`~/.asg/tools`; `asg tool add <folder|github:owner/repo[/path]>` imports them.

```json
{
  "name": "word_count",
  "description": "Count the words in a text file. Use when the user asks how long a document is.",
  "parameters": {
    "type": "object",
    "required": ["path"],
    "properties": { "path": { "type": "string", "description": "File to count" } }
  },
  "runtime": "python",
  "entry": "main.py",
  "needs_approval": false,
  "timeout": 60
}
```

- `runtime` is `python` or `go`. Python tools run with `uv run` on ASG's
  managed Python 3.12; libraries from `pyproject.toml`, `requirements.txt` or
  PEP 723 inline metadata — or, for a plain script that declares nothing,
  from its `import` lines ([infer_imports.py](../internal/tools/infer_imports.py)) —
  are installed into the tool's own environment on first run. Go tools (`go.mod` + `main.go`) are built on first run with the
  managed Go (installed on demand) and rebuilt when the source changes.
  See [internal/runtimes](../internal/runtimes) and [docs/tools.md](../docs/tools.md).
- `command` still runs any other program; `{dir}` is replaced with the tool's
  folder. `["python", "{dir}/run.py"]` now runs on the managed Python.
- The call's arguments arrive as JSON on **stdin** (and in `$ASG_TOOL_ARGS`).
- Whatever the program prints to stdout is the result the model sees.
- A non-zero exit is reported to the model as an error, with stderr.
- `needs_approval` defaults to `true` — the user is asked before each run
  (or YOLO is on). Only set it to `false` for tools that change nothing.
- A project tool (`.asg/tools`) wins over a user tool with the same name;
  neither can replace a built-in or bundled tool.

Share a script tool by committing it to a project's `.asg/tools/` folder,
or propose it here as a bundled tool so every user gets it.
