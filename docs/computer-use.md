# Computer use

With computer use on, ASG can **see your screen and use your real mouse and
keyboard**: click, double-click, drag, scroll, type, press shortcuts, switch
windows and open apps. It is for jobs that have no command line or API — a
desktop app, a settings dialog, a web page that needs a person's clicks.

It works in a loop, one step at a time:

1. take a screenshot and look at it
2. decide one action (click the Save button, type a name, press `ctrl+s`)
3. do it — you see the pointer move
4. a fresh screenshot comes back, with how much of the screen changed, so ASG
   can check the action worked before the next one

It is **off until you turn it on**, and every mouse or keyboard action asks
first unless you choose otherwise.

## Turn it on

| Where | How |
|---|---|
| Terminal | `/computer on` (`/computer` shows the settings, `/computer off` turns it off) |
| Desktop | **Customise → Computer use → Allow computer use**, then **Save** |
| Config | `"computer": { "enabled": true }` in `~/.asg/config.json` |

Then ask for something in **build** or **loop** mode:

```text
open Notepad, write a shopping list with eggs, milk and bread, and save it to my desktop as list.txt
in the Settings app, turn on dark mode
```

Plan and ask modes never get the tool — they change nothing.

You need a **vision model** (one that reads images), or set a separate
perception model (below) and keep a text-only chat model for planning.

## Staying in control

| You want to… | Do this |
|---|---|
| **Stop at once** | press **Esc** (Windows), click **Stop** on the desktop banner, press **ctrl+c** in the terminal, or stop it from your phone |
| **Take over** | just grab the mouse or press a key — ASG pauses mid-action, and asks you before it carries on |
| **Pause / resume** | the **Pause** / **Resume** buttons on the desktop banner |

While ASG is in control you see it:

- **Windows**: an amber frame around the monitor it works on (grey while paused).
  The frame is hidden from its own screenshots.
- **Desktop app**: a banner at the top with the last action, Pause and Stop.
- **Terminal**: a `🖱 computer` badge in the status line.

If you use the mouse or keyboard between ASG's actions (say, to click **Allow**
on a permission prompt), ASG waits until you have stopped before it moves again.
If you keep going, it pauses.

## Permissions

Every call goes through ASG's normal [permission gate](permissions.md). The
`approval` setting decides what asks:

| `approval` | Looking (screenshots, window list) | Mouse and keyboard |
|---|---|---|
| `actions` (default) | free | asks — "always allow" covers one kind (`computer:mouse`, `computer:keyboard`, `computer:windows`, `computer:clipboard`) |
| `every` | asks | asks |
| `auto` | free | free |

**Sensitive actions always ask, every time — even in YOLO mode, and even with
`auto`.** They are never remembered and scheduled (unattended) runs skip them.
An action is sensitive when ASG flags it (sending, submitting, buying, deleting,
closing unsaved work) or when it is a shortcut that closes, quits, deletes or
locks: `alt+f4`, `ctrl+w`, `ctrl+q`, `cmd+q`, `shift+delete`, `win+l`, ….

ASG can never click or type into **its own windows** (the desktop app, or the
terminal it runs in, where Windows can tell): it could otherwise approve its own
permission prompts.

## Excluding apps and monitors

- **Excluded apps** (`excluded_apps`): app names or window-title words, such as
  `keepass, 1password, bank`. Their windows are **blacked out** of every
  screenshot, and ASG refuses to click or type into them.
- **Excluded monitors** (`excluded_monitors`): never captured or touched.
  In the desktop settings, right-click a monitor to exclude it.

## Monitors and coordinates

`list_monitors` numbers the displays: `0` is the primary monitor, then the
others left to right; `-1` is all of them as one wide image. ASG works on the
`monitor` you choose (default `0`) and can switch with `screenshot monitor=1`.

Screenshots are scaled so the longer side is at most `max_image_side` pixels
(default 1280). ASG gives coordinates in that image and they are scaled back
onto the real screen, HiDPI included. For small text it can `zoom` into a
region at full resolution.

## A separate vision model

Set `vision_model` (and optionally `vision_provider`) to let a different model
**look** while your chat model **plans**: each screenshot is described in text —
the active window, every button, field and link with its coordinates — and only
that text goes to the chat model. Useful when:

- your chat model can't read images, or
- you want a local vision model (e.g. an Ollama `qwen2.5vl`) doing the looking.

If the vision model fails, the image is sent to the chat model instead.

## What it can do

| Action | Arguments |
|---|---|
| `screenshot` | `monitor`, `focus` (what to look for — helps a vision model) |
| `zoom` | `x, y, to_x, to_y` — read a region at full resolution |
| `list_monitors`, `list_windows`, `cursor_position` | |
| `mouse_move`, `left_click`, `right_click`, `middle_click`, `double_click`, `triple_click` | `x, y` |
| `click` | `x, y, button, clicks` |
| `mouse_down`, `mouse_up` | `button`, optional `x, y` — hold and release |
| `drag` | `x, y` → `to_x, to_y` |
| `scroll` | `x, y, direction` (up/down/left/right), `amount` |
| `type` | `text` |
| `key` | `keys`: `"ctrl+s"`, `"alt+tab"`, `"enter"`, or several: `"ctrl+a ctrl+c"` |
| `key_down`, `key_up` | `keys` — hold and release (released automatically when the turn ends) |
| `focus_window` | `window`: an id from `list_windows` or words from the title |
| `open` | `target`: an app name, file, folder or URL |
| `clipboard_get`, `clipboard_set` | `text` |
| `wait` | `seconds`, then a screenshot |

Only the three newest screenshots stay in the conversation; older ones are
replaced by a note, so long sessions don't fill the model's context.

## Platforms

| OS | How | Needs |
|---|---|---|
| **Windows** | native (user32 / gdi32): capture, input, windows, the frame and the Esc stop | nothing. Windows won't let ASG type into apps running **as administrator** unless ASG runs as administrator too |
| **Linux** | X11: `xdotool` (input), `xrandr` (monitors), `wmctrl` (windows), the usual screenshot tools | `sudo apt install xdotool wmctrl` and an X11 session — Wayland doesn't accept synthetic input |
| **macOS** | `screencapture`, `cliclick` (mouse), System Events (keyboard, windows) | `brew install cliclick`, and allow **Screen Recording** and **Accessibility** for your terminal or ASG in System Settings → Privacy & Security. Scrolling uses the arrow keys; there is no middle button |

On Linux and macOS there is no global stop key and no on-screen frame: use the
Stop button or ctrl+c, and moving the mouse pauses control.

## Settings

All under `"computer"` in `~/.asg/config.json`:

```json
"computer": {
  "enabled": true,
  "monitor": 0,
  "approval": "actions",
  "max_actions": 200,
  "session_minutes": 30,
  "max_image_side": 1280,
  "move_ms": 250,
  "no_auto_screenshot": false,
  "vision_provider": "",
  "vision_model": "",
  "excluded_apps": ["keepass"],
  "excluded_monitors": []
}
```

| Key | Meaning |
|---|---|
| `enabled` | computer use on or off (default off) |
| `monitor` | default monitor: `0` primary, `1…` others, `-1` all |
| `approval` | `actions` · `every` · `auto` (see [Permissions](#permissions)) |
| `max_actions` | mouse/keyboard actions allowed in one turn (default 200) |
| `session_minutes` | control ends this long after a turn's first action (default 30) |
| `max_image_side` | longer side of screenshots sent to the model, 640–2560 (default 1280) |
| `move_ms` | how long a visible pointer move takes; `-1` jumps (default 250) |
| `no_auto_screenshot` | `true` skips the check screenshot after each action (faster, blind) |
| `vision_provider`, `vision_model` | a separate [perception model](#a-separate-vision-model) |
| `excluded_apps`, `excluded_monitors` | see [Excluding apps and monitors](#excluding-apps-and-monitors) |
