#!/usr/bin/env python3
"""Sync markdown files from docs/ into website/docs/docs-data.js.

Zero third-party dependencies. Run from anywhere in the repo:
    python website/scripts/sync-docs.py
"""
from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
DOCS_DIR = ROOT / "docs"
OUT_FILE = ROOT / "website" / "docs" / "docs-data.js"

PAGES = [
    {
        "slug": "overview",
        "file": "README.md",
        "title": "Overview",
        "group": "Start here",
        "summary": "The two switches (mode + domain) and a two-minute tour of ASG.",
        "accent": "lime",
    },
    {
        "slug": "getting-started",
        "file": "getting-started.md",
        "title": "Getting started",
        "group": "Start here",
        "summary": "Starting asg, reading the screen, the prompt, and everyday keys.",
        "accent": "lime",
    },
    {
        "slug": "modes",
        "file": "modes.md",
        "title": "Modes",
        "group": "Core workflow",
        "summary": "How ASG works: build, plan, loop, and ask — switched with Shift+Tab.",
        "accent": "cyan",
    },
    {
        "slug": "domains",
        "file": "domains.md",
        "title": "Domains",
        "group": "Core workflow",
        "summary": "Software, docs, cyber, devops, pm — and their flavors.",
        "accent": "magenta",
    },
    {
        "slug": "loop",
        "file": "loop.md",
        "title": "Loop harness",
        "group": "Core workflow",
        "summary": "Run an objective step by step until your tests verify it.",
        "accent": "coral",
    },
    {
        "slug": "review-and-questions",
        "file": "review-and-questions.md",
        "title": "Self-review & questions",
        "group": "Core workflow",
        "summary": "Four review passes after every answer, and how ask_user pauses for you.",
        "accent": "magenta",
    },
    {
        "slug": "work-folder",
        "file": "work-folder.md",
        "title": "Work folder",
        "group": "Core workflow",
        "summary": "The plan, checklist, walkthrough, and activity log in .asg/work/.",
        "accent": "cyan",
    },
    {
        "slug": "tools",
        "file": "tools.md",
        "title": "Tools",
        "group": "Tools & safety",
        "summary": "Built-in tools, Office documents, screenshots, and custom script tools.",
        "accent": "cyan",
    },
    {
        "slug": "permissions",
        "file": "permissions.md",
        "title": "Permissions & YOLO",
        "group": "Tools & safety",
        "summary": "Approval gates, always-allow rules, and unattended YOLO mode.",
        "accent": "coral",
    },
    {
        "slug": "commands",
        "file": "commands.md",
        "title": "Command reference",
        "group": "Reference",
        "summary": "Every slash command and keyboard shortcut on one page.",
        "accent": "lime",
    },
    {
        "slug": "cli-and-config",
        "file": "cli-and-config.md",
        "title": "CLI & config",
        "group": "Reference",
        "summary": "CLI flags, one-shot runs, and ~/.asg/config.json.",
        "accent": "cyan",
    },
    {
        "slug": "desktop",
        "file": "desktop.md",
        "title": "Desktop app",
        "group": "Reference",
        "summary": "Saved sessions, projects, coding sessions, artifacts, and schedules.",
        "accent": "magenta",
    },
]


def main() -> None:
    entries = []
    for meta in PAGES:
        src = DOCS_DIR / meta["file"]
        if not src.is_file():
            raise FileNotFoundError(f"Missing doc file: {src}")
        md = src.read_text(encoding="utf-8").replace("\r\n", "\n")
        entries.append({**meta, "md": md})

    OUT_FILE.parent.mkdir(parents=True, exist_ok=True)
    payload = json.dumps(entries, ensure_ascii=False, indent=2)
    OUT_FILE.write_text(
        "// Auto-generated from docs/*.md by website/scripts/sync-docs.py — do not edit by hand.\n"
        f"window.ASG_DOCS = {payload};\n",
        encoding="utf-8",
    )
    print(f"Synced {len(entries)} docs -> {OUT_FILE.relative_to(ROOT)} ({OUT_FILE.stat().st_size:,} bytes)")


if __name__ == "__main__":
    main()
