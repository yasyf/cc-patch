# cc-patch Development Guide

Fast mode for Claude Code's delegated agents, re-applied automatically on every update.

## Repository Structure

```
cc-patch/
├── cmd/cc-patch/   # main package — the CLI entry point
├── internal/
│   ├── cli/               # cobra command tree (apply, status, restore, heal, list, *-daemons)
│   ├── claude/            # locate the installed Claude Code binary + backup path
│   ├── macho/             # find a named Mach-O segment's file offset (thin + fat)
│   ├── binpatch/          # length-neutral in-place byte edits, backup, ad-hoc re-sign
│   ├── registry/          # the registered patches + structural site derivation
│   ├── patcher/           # apply a patch: stored override → pinned literals → derive
│   ├── heal/              # re-derive a drifted patch through `claude -p`
│   ├── store/             # persist heal-derived sites per Claude version
│   ├── daemon/            # launchd agents (WatchPaths re-patch + daily heal) via daemonkit
│   ├── version/           # build version, stamped via -ldflags
│   └── log/               # slog setup
├── .github/               # GitHub Actions workflows
├── AGENTS.md              # This file — shared conventions
└── README.md              # Project overview
```
