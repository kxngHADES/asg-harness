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

## Your own tools: script tools

Add a tool without touching Go: create a folder with a `tool.json` and a script
in **any language**.

```sh
asg tool new word_count        # or /tool new word_count inside asg
```

That creates `~/.asg/tools/word_count/` with a `tool.json` and a `run.py`
starter. Edit both, then run `/tools` to reload. You don't need to restart.

```json
{
  "name": "word_count",
  "description": "Count the words in a text file. Use when the user asks how long a document is.",
  "parameters": {
    "type": "object",
    "required": ["path"],
    "properties": { "path": { "type": "string", "description": "File to count" } }
  },
  "command": ["python", "{dir}/run.py"],
  "needs_approval": false,
  "timeout": 60
}
```

```python
# run.py: arguments arrive as JSON on stdin; print the result
import json, sys
args = json.load(sys.stdin)
print(len(open(args["path"], encoding="utf-8").read().split()), "words")
```

| Field | Meaning |
|---|---|
| `name` | what the model calls it (letters, digits, `_`) |
| `description` | **when** the model should use it; write this carefully |
| `parameters` | JSON Schema of the arguments |
| `command` | program and args; `{dir}` is the tool's folder |
| `needs_approval` | defaults to `true` (asks before each run). Set `false` only for tools that change nothing |
| `timeout` | seconds |

The script gets the arguments on **stdin** and in `$ASG_TOOL_ARGS`, and its
folder in `$ASG_TOOL_DIR`. Whatever it prints to stdout is the result. A
non-zero exit is reported as an error, with stderr.

| Folder | Who gets the tool |
|---|---|
| `~/.asg/tools/<name>/` | you, in every project |
| `<project>/.asg/tools/<name>/` | everyone who works in that project (commit it) |

A project tool wins over a user tool with the same name. Neither can replace a
built-in or bundled tool.

```sh
asg tool list                   # same as /tools, from the shell
```

To ship a tool to **every** ASG user, add it to the repo's
[`tools/`](../tools/README.md) folder as a bundled Go tool.

Next: [Permissions and YOLO](permissions.md)
