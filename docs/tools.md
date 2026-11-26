# Tools

Tools are what the model can *do*. You don't call them yourself: the model
picks them, and you see each call as a `◆ TOOL` line (and approve the ones
that change things).

```text
/tools          list every tool, where it came from, and whether it asks first
```

```
  bash             builtin  asks   Run a shell command …
  read_file        builtin         Read a file …
  office_document  bundled  asks   Create a Word (.docx), PowerPoint (.pptx) or Excel (.xlsx) file …
  screenshot       bundled  asks   Take a screenshot and look at it …
  word_count       script          Count the words in a text file …
  ask_user         builtin         pause and ask you a question whenever information is missing
  script tools: E:\code\myapp\.asg\tools · C:\Users\you\.asg\tools
```

## Built-in tools

| Tool | Does | Asks first |
|---|---|---|
| `bash` | runs a shell command | yes |
| `read_file` | reads a file | no |
| `write_file` | creates or overwrites a file | yes (not in the work folder) |
| `edit_file` | replaces exact text in a file | yes (not in the work folder) |
| `list_dir`, `glob`, `grep` | look around the project | no |
| `web_fetch` | fetches a URL | yes |
| `ask_user` | [asks you a question](review-and-questions.md#ask_user) | no |
| `use_skill`, `memory_save`, `memory_delete` | skills and memories | no |

In plan and ask modes the model only gets the read-only tools, plus
`web_fetch`, `screenshot`, `ask_user` and the memory and skill tools.

## office_document

Writes **Word `.docx`**, **PowerPoint `.pptx`** and **Excel `.xlsx`** files.
It's built into ASG in pure Go, so you don't need Office, LibreOffice or Python.
The file extension picks the format. Just ask for the file:

```text
write the release notes for v0.2 to docs/release-0.2.docx
turn docs/getting-started.md into a 6-slide deck at talk.pptx
put the benchmark results in results.xlsx with a total row
/cyber blue … and give me the report as report.docx too
/pm sprint … export the sprint plan as sprint.xlsx
```

| Format | Built from | Supports |
|---|---|---|
| `.docx` | Markdown | headings, bold/italic/code, bullet and numbered lists (nested), tables, quotes, code blocks, page breaks |
| `.pptx` | Markdown (a slide per `#`/`##` heading) or exact slides | title slides, bullets with sub-levels |
| `.xlsx` | sheets of rows, or CSV | numbers stay numbers, `=` formulas, a bold frozen header row, several sheets |

The files open in Word, PowerPoint, Excel, LibreOffice and Google Docs.

## screenshot

Shows the model **what something looks like**. It renders a target and attaches
the image to the conversation:

| Target | How it's captured |
|---|---|
| `http://…` / `https://…` | headless Chrome, Edge or Chromium |
| `docs/page.md` | rendered to HTML (relative images work), then captured |
| `page.html` | opened in the headless browser |
| `screen` | the whole screen (Windows, macOS, Linux with grim/gnome-screenshot/…) |

```text
/docs update screenshot docs/install.md and fix whatever renders badly
how does http://localhost:3000/pricing look on a 390px wide screen?
```

- You need a model that can **read images**. With a text-only model, the image
  is wasted.
- If no browser is found, set `ASG_BROWSER` to the browser's executable path.
- It asks first (it can open URLs).

## computer

Live [computer use](computer-use.md): screenshots of your desktop plus the real
mouse and keyboard (click, drag, scroll, type, shortcuts, windows, clipboard).
Off until you turn it on with `/computer on` or in the desktop's Customise page;
mouse and keyboard actions ask first, and sensitive ones always ask.

## Your own tools: Python and Go

Add a tool without touching ASG's code: a folder with a `tool.json` and a
small Python or Go project. ASG runs them on a **runtime it manages itself**
(uv with Python 3.12, and Go), so they work on any machine without installing
Python or Go first.

```sh
asg tool new word_count            # Python (or /tool new word_count inside asg)
asg tool new word_count --go       # Go     (or /tool new word_count go)
```

That creates `~/.asg/tools/word_count/` with a `tool.json` and a starter
project: `pyproject.toml` + `main.py`, or `go.mod` + `main.go`. Edit them, then
run `/tools` to reload. You don't need to restart. In the desktop app use
**Customise → Tools → New tool**.

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

```python
# main.py: arguments arrive as JSON on stdin; print the result
import json, sys
args = json.load(sys.stdin)
print(len(open(args["path"], encoding="utf-8").read().split()), "words")
```

| Field | Meaning |
|---|---|
| `name` | what the model calls it (letters, digits, `_`, `-`) |
| `description` | **when** the model should use it; write this carefully |
| `parameters` | JSON Schema of the arguments |
| `runtime` | `python` or `go`. Left out, ASG guesses from the files (`go.mod` → Go; `pyproject.toml`, `requirements.txt` or `*.py` → Python) |
| `entry` | the Python script (default `main.py`, then `run.py`) or Go package (default `.`) |
| `args` | extra command-line arguments for the entry |
| `command` | run any other program instead, e.g. `["node", "{dir}/index.js"]`; `{dir}` is the tool's folder. `["python", "x.py"]` runs on the managed Python |
| `needs_approval` | defaults to `true` (asks before each run). Set `false` only for tools that change nothing |
| `timeout` | seconds the run may take (first-time setup is not counted) |

The program gets the arguments on **stdin** and in `$ASG_TOOL_ARGS`, and its
folder in `$ASG_TOOL_DIR`. It runs in your project folder, so relative paths
work. Whatever it prints to stdout is the result. A non-zero exit is reported
as an error, with stderr.

### Libraries

| Tool | Where its libraries come from | When they're installed |
|---|---|---|
| Python with `pyproject.toml` | `dependencies = [...]` | `uv sync` on first run, again after you edit the file |
| Python with `requirements.txt` | the requirements file | `uv pip install -r` on first run, again after edits |
| A single Python script | [PEP 723](https://peps.python.org/pep-0723/) `# /// script` block | by `uv run`, cached |
| A plain Python script (nothing declared) | its `import` lines — see below | on first run, again after the scripts change |
| Go | `go.mod` (add with `go get`) | `go build` on first run; rebuilt when the source changes |

**Plain scripts.** Most scripts that ship with skills (Anthropic's pdf, docx,
pptx and xlsx skills, for example) declare nothing; they just `import pypdf`,
`from PIL import Image`, `import yaml`. For those ASG reads the imports with
Python's own parser, starting at the script and following its local modules
(so `helpers.py`, an `office/` package or `scripts.utils` are never mistaken
for PyPI packages), skips the standard library, maps import names to PyPI
names (`PIL` → `pillow`, `yaml` → `pyyaml`, `docx` → `python-docx`, `cv2` →
`opencv-python`, …) and installs them. Imports inside `try/except
ImportError` are installed if possible and skipped if not. If a run still
fails with `ModuleNotFoundError` (a dynamic import the scan can't see), ASG
installs that module and runs the script again. Declare dependencies
(`pyproject.toml`, `requirements.txt` or a PEP 723 block) when you need exact
versions; ASG then uses those instead of guessing.

Every Python tool gets **its own virtual environment** (in
`~/.asg/runtime/envs/`, not in the tool folder), so two tools never fight over
library versions and tool folders stay clean to share. Installs happen when a
tool is added and on its first run; ASG shows what it's doing while it waits.
`asg tool setup <name>` (or **Set up** in the desktop app) does it ahead of time.

### Importing tools

```sh
asg tool add github:owner/repo              # every folder with a tool.json in the repo
asg tool add https://github.com/owner/repo/tree/main/tools/pdf_text
asg tool add ~/Downloads/my-tools           # a local folder
```

`/tool add <source>` does the same inside asg, and the desktop app has
**Customise → Tools → Add tool**. Imported tools go to `~/.asg/tools/` and
their libraries are installed straight away.

| Folder | Who gets the tool |
|---|---|
| `~/.asg/tools/<name>/` | you, in every project |
| `<project>/.asg/tools/<name>/` | everyone who works in that project (commit it) |
| `<skill>/tools/<name>/` | everyone who installs that skill |

A project tool wins over a user tool with the same name. Neither can replace a
built-in or bundled tool.

```sh
asg tool list                   # same as /tools, from the shell
```

### Skill scripts

Skills can ship Python or Go scripts with no project file of their own. The
built-in `run_script` tool runs them on the managed runtime, installing
dependencies from a `pyproject.toml` or `requirements.txt` next to the script
(or one folder up), from PEP 723 inline metadata, or — for a plain script —
from its imports. When a skill is used, ASG tells the model to run its scripts
with `run_script` rather than through the shell. A skill can also ship
full tools in a `tools/` folder.

### The runtime

```sh
asg runtime                     # what's installed (/runtime inside asg)
asg runtime install [--go]      # install or repair uv + Python 3.12 (and Go)
```

Everything lives in `~/.asg/runtime/`: `uv` is downloaded from its official
GitHub release and checked against the published SHA-256, uv installs
Python 3.12 there, and Go comes from your `PATH` or, if you have none, is
downloaded from go.dev (checksum-verified) the first time a Go tool runs.
The desktop app sets up uv and Python in the background on a new install and
after every update.

| Variable | Effect |
|---|---|
| `ASG_UV` | use this uv binary instead of downloading one |
| `ASG_UV_VERSION` | pin the uv release (e.g. `0.11.19`) instead of the latest |
| `ASG_MANAGED_GO_ONLY=1` | ignore Go on `PATH` and use ASG's own |
| `ASG_NO_RUNTIME_BOOTSTRAP=1` | desktop app: don't set the runtime up at startup |

To ship a tool to **every** ASG user, add it to the repo's
[`tools/`](../tools/README.md) folder as a bundled Go tool.

Next: [Permissions and YOLO](permissions.md)
