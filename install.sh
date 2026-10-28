#!/bin/sh
# ASG installer for macOS and Linux (bash, zsh, fish, sh — any terminal).
#
#   curl -fsSL https://raw.githubusercontent.com/kxngHADES/asg-harness/main/install.sh | sh
#
# Environment:
#   ASG_VERSION=v0.2.0      install a specific release (default: latest)
#   ASG_INSTALL_DIR=~/bin   install location (default: /usr/local/bin if
#                           writable, else ~/.local/bin)
set -eu

REPO="kxngHADES/asg-harness"
VERSION="${ASG_VERSION:-latest}"

say() { printf '\033[38;5;154m▸\033[0m %s\n' "$*"; }
die() { printf '\033[38;5;203m✕\033[0m %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin | linux) ;;
  msys* | mingw* | cygwin*) die "On Windows run: irm https://raw.githubusercontent.com/$REPO/main/install.ps1 | iex" ;;
  *) die "unsupported OS: $os" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  base="https://github.com/$REPO/releases/download/$VERSION"
fi
asset="asg_${os}_${arch}.tar.gz"

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    die "need curl or wget"
  fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "downloading $asset ($VERSION)"
if ! fetch "$base/$asset" "$tmp/$asset"; then
  if command -v go >/dev/null 2>&1; then
    say "no release binary found — building from source with go install"
    GOBIN="${ASG_INSTALL_DIR:-$HOME/.local/bin}" go install "github.com/$REPO/cmd/asg@${ASG_VERSION:-latest}"
    say "installed to ${ASG_INSTALL_DIR:-$HOME/.local/bin}/asg"
    exit 0
  fi
  die "download failed: $base/$asset"
fi
fetch "$base/checksums.txt" "$tmp/checksums.txt"

expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
[ -n "$expected" ] && [ "$expected" = "$actual" ] || die "checksum mismatch for $asset"

tar -xzf "$tmp/$asset" -C "$tmp"

dir="${ASG_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"
install -m 755 "$tmp/asg" "$dir/asg" 2>/dev/null || { cp "$tmp/asg" "$dir/asg" && chmod 755 "$dir/asg"; }
say "installed $("$dir/asg" version) to $dir/asg"

# shellcheck disable=SC2016 # $PATH stays literal in the printed hints
case ":$PATH:" in
  *":$dir:"*) ;;
  *)
    printf '\n%s is not on your PATH. Add it:\n' "$dir"
    printf '  bash:  echo '\''export PATH="%s:$PATH"'\'' >> ~/.bashrc\n' "$dir"
    printf '  zsh:   echo '\''export PATH="%s:$PATH"'\'' >> ~/.zshrc\n' "$dir"
    printf '  fish:  fish_add_path %s\n\n' "$dir"
    ;;
esac
say "run 'asg' for the terminal agent, or 'asg dashboard' for the web version"
