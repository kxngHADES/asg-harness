# Install

ASG comes in three parts. Install the ones you need:

| Part | What it is | Install |
|---|---|---|
| **Terminal app** (`asg`) | The coding agent in your terminal | One command, below |
| **Desktop app** | Windowed app with saved sessions, projects, artifacts and schedules | Setup wizard, below |
| **Mobile companion** | Android / iOS app that connects to ASG Desktop on your network | APK or IPA, below |

All downloads come from the [latest GitHub release](https://github.com/kxngHADES/asg-harness/releases/latest).

## 1. Install the terminal app

Pick your platform and paste the line into a terminal.

**macOS / Linux** (bash or zsh)

```sh
curl -fsSL https://raw.githubusercontent.com/kxngHADES/asg-harness/main/install.sh | sh
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/kxngHADES/asg-harness/main/install.ps1 | iex
```

**Any OS with Go 1.26+**

```sh
go install github.com/kxngHADES/asg-harness/cmd/asg@latest
```

The script picks your OS and CPU, checks the SHA-256 checksum and puts `asg` on your `PATH`:

- macOS / Linux: `/usr/local/bin`, or `~/.local/bin` if that isn't writable (it prints the `PATH`
  line to add if needed).
- Windows: `%LOCALAPPDATA%\Programs\asg`, added to your user `PATH`. Open a new terminal afterwards.

Options (set before running the script):

| Variable | Does |
|---|---|
| `ASG_VERSION=v0.2.0` | install a specific release instead of the latest |
| `ASG_INSTALL_DIR=~/bin` | install to a different folder |

On Windows set them first, e.g. `$env:ASG_VERSION = "v0.2.0"`.

### Check it worked

```sh
asg version
```

If the command isn't found, open a new terminal (or add the folder the script printed to your `PATH`).

## 2. Set up a model

ASG needs a model to talk to. Choose one:

**Local, free — Ollama.** Install [Ollama](https://ollama.com/download), then pull a model:

```sh
ollama pull qwen3.5:9b
```

ASG uses Ollama out of the box, so there's nothing else to configure.

**Hosted — Anthropic, OpenAI or any OpenAI-compatible API** (OpenRouter, Groq, LM Studio, …).
All you need is an API key. Start `asg`, type `/provider`, pick the provider and enter your key.
Your choice is saved in `~/.asg/config.json` and shared with the desktop app.

## 3. Start ASG

```sh
cd your-project
asg
```

Run it from the folder you want to work on. Ask for something; when ASG wants to run a command or
change a file it stops and asks first. Run `/init` once so ASG writes an `ASG.md` about your project.

Next: [Getting started](getting-started.md) walks through the screen, the prompt and the keys.

## Desktop app (optional)

Download the setup file for your platform and run it:

| File | Platform | First run |
|---|---|---|
| `asg-setup_windows_amd64.exe` / `_arm64.exe` | Windows 10/11 | Not code-signed: if SmartScreen says "Windows protected your PC", choose **More info → Run anyway**. |
| `asg-setup_darwin_universal.dmg` | macOS 11+ | Open the disk image, then **right-click → Open** on **ASG Setup** the first time. |
| `asg-setup_linux_amd64` / `_arm64` | Linux with GTK 3 + WebKitGTK 4.1 | `chmod +x asg-setup_linux_amd64 && ./asg-setup_linux_amd64` |

The wizard asks where to install, which shortcuts to add, where to keep your data, and which
providers to switch on. Zip archives and every detail are in [Desktop app](desktop.md).

## Mobile companion (optional)

The mobile app connects to ASG Desktop over your local network, so install the desktop app first.

| File | Platform | How |
|---|---|---|
| `asg-mobile_android.apk` | Android | Download on the phone and open it. Allow installs from your browser when Android asks. |
| `asg-mobile_ios_unsigned.ipa` | iOS | Unsigned: sideload with AltStore or Xcode. |

## Update

```sh
asg update
```

upgrades the terminal app in place. The desktop app updates itself from **Settings → Updates**.

## Verify a download

Every release file has a matching `.sha256` (terminal archives are listed in `checksums.txt`).
The install scripts check this for you; for manual downloads:

```sh
sha256sum asg-mobile_android.apk          # Linux
shasum -a 256 asg-mobile_android.apk      # macOS
Get-FileHash asg-mobile_android.apk       # Windows PowerShell
```

Compare the result with the `.sha256` file.

## Build from source

```sh
git clone https://github.com/kxngHADES/asg-harness && cd asg-harness
make install        # builds to ~/.local/bin/asg
```
