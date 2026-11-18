# Command line and config

## Subcommands

```sh
asg                          # interactive terminal agent
asg -p "prompt" [img...]     # one prompt, print the answer ("-" reads stdin)
asg -loop -yolo -p "goal"    # run an objective with the loop harness, unattended
asg dashboard [-no-open]     # browser dashboard on 127.0.0.1
asg skill add <source>       # import skills (path, URL, github:owner/repo[/path])
asg skill list | rm <name>
asg tool new <name>          # create a script tool in ~/.asg/tools
asg tool list                # list the tools the model can call
asg update                   # update to the latest release
asg version
```

## Flags

| Flag | Meaning | Saved? |
|---|---|---|
| `-provider <name>` | provider to use | yes |
| `-model <name>` | model to use | yes |
| `-mode build\|plan\|loop\|ask` | starting mode | no |
| `-domain <domain>[/<flavor>]` | e.g. `software/work`, `cyber/blue`, `devops` | yes |
| `-review <n>` | self-review passes (`-1` = off) | yes |
| `-p "<prompt>"` | one-shot prompt (`-` reads stdin) | no |
| `-loop` | with `-p`: treat the prompt as a loop objective | no |
| `-verify "<cmd>"` | with `-loop`: check command (`none` to skip) | no |
| `-yolo` | run tools without asking, this run only | no |
| `-no-open` | with `dashboard`: don't open a browser | no |
| `-version` | print the version | no |

## Examples

```sh
# Ask a question about the repo, no changes
asg -mode ask -p "where are HTTP timeouts configured?"

# Review the CI pipelines and print the findings
asg -domain devops/review -mode ask -p "review .github/workflows for security problems"

# Plan a sprint
asg -domain pm/sprint -mode plan -p "plan the next sprint from TODO.md for 2 developers"

# Build a feature unattended with the strict work rules
asg -domain software/work -loop -yolo -verify "go test ./..." \
    -p "add a --json flag to the list command, with tests"

# Read a long prompt from a file or another command ("-" = stdin)
asg -mode ask -p - < question.txt

# Attach images
asg -p "what's wrong with this layout?" screenshot.png
```

With `-p` in a terminal, [`ask_user`](review-and-questions.md#ask_user)
questions are asked on the terminal. When input is piped in, the model makes a
stated assumption instead.

## Config file

`~/.asg/config.json` (set `ASG_HOME` to move the whole `~/.asg` folder). The
terminal, `asg` flags and the dashboard all edit the same file.

```json
{
  "active_provider": "ollama",
  "providers": {
    "ollama":    { "type": "ollama",    "base_url": "http://localhost:11434",    "model": "qwen3.5:9b", "num_ctx": 16384 },
    "anthropic": { "type": "anthropic", "base_url": "https://api.anthropic.com", "api_key": "sk-ant-…", "model": "claude-sonnet-5-5" }
  },
  "auto_approve": false,
  "domain": "software",
  "flavor": "work",
  "review": { "passes": 4, "max_passes": 6 },
  "loop": { "max_iterations": 30, "step_attempts": 3, "step_rounds": 15, "verify": "" }
}
```

| Key | Meaning |
|---|---|
| `active_provider` | which entry of `providers` is used |
| `providers.<name>.model` | that provider's model |
| `providers.<name>.num_ctx` | Ollama context window (default 16384; negative = Ollama's default) |
| `auto_approve` | YOLO on start (turning YOLO off clears it) |
| `domain`, `flavor` | the [track](domains.md) ASG starts in |
| `review.passes` | minimum self-review passes (`-1` = off) |
| `review.max_passes` | upper limit when the last pass still finds problems |
| `loop.*` | [loop tuning](loop.md#tuning-asgconfigjson) |

`/config` in the terminal shows the file's path and every active value.

## Other files

| Path | What |
|---|---|
| `~/.asg/permissions.json` | remembered "always allow" rules |
| `~/.asg/loops/` | saved loop runs (`/loop resume`) |
| `~/.asg/tools/` | your script tools |
| `~/.asg/skills/` | installed skills |
| `<project>/ASG.md` | project instructions (`/init`) |
| `<project>/.asg/tools/` | the project's script tools |
| `<project>/.asg/work/` | [work folders](work-folder.md) |
