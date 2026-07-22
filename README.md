# cc-patch

Keep Claude Code's fast mode on for delegated agents, across every update.

Fast mode (priority-tier Opus) applies to your top-level session but not to the
agents it spawns — subagents, agent-team teammates, and workflow branches all run
at standard speed. cc-patch patches the installed Claude Code binary so delegated
Opus agents run fast too, and re-applies itself whenever an auto-update replaces
the binary.

[![CI](https://img.shields.io/github/actions/workflow/status/yasyf/cc-patch/ci.yml?branch=main&label=ci)](https://github.com/yasyf/cc-patch/actions/workflows/ci.yml)

## Get started

```bash
git clone https://github.com/yasyf/cc-patch
cd cc-patch
task build   # -> ./bin/cc-patch

./bin/cc-patch status   # is the installed Claude Code binary patched?
```

`status` reads `~/.local/bin/claude`, resolves it to the versioned binary, and
reports whether each registered patch is applied — it never writes.

## Apply the patch

```bash
cc-patch apply --all       # patch the current binary and re-sign it
cc-patch restore           # roll back to the pristine, vendor-signed binary
```

`apply` edits the binary in place (a length-neutral byte overwrite, no segment
resize), backs the original up to `<binary>.ccpatch-orig`, and re-signs the result
ad-hoc. It is idempotent — a second `apply` reports `already patched`.

Only Opus-family delegated agents are affected: the patch keeps Claude Code's own
model-eligibility gate, so sonnet and fable agents still run at standard speed and
the server never sees a fast request it would reject.

> **Re-signing drops the vendor signature.** The edit invalidates Anthropic's
> hardened-runtime signature, so `apply` replaces it with an ad-hoc signature.
> Any MDM or security tooling that checks the TeamID will see the difference.
> `cc-patch restore` returns the original, vendor-signed binary byte for byte.

Fast mode bills at the priority tier, so this raises spend for Opus subagents,
teammates, and workflow agents. That is the intended effect, bounded to your Opus
delegated work.

## Stay patched across updates

Claude Code auto-updates by dropping a new versioned binary and repointing the
launcher symlink, which reverts the patch. Two launchd agents keep it applied:

```bash
cc-patch install-daemons     # a WatchPaths re-patcher + a daily heal job
cc-patch uninstall-daemons   # remove both
```

The watcher fires whenever `~/.local/share/claude/versions` or the launcher
changes, and runs `cc-patch apply --all` against the new binary. The heal job
runs `cc-patch heal --all` once a day.

`heal` re-applies each patch, escalating only when an update has shifted the code:
first it re-locates the patch sites structurally (tolerating renamed locals), and
only if that fails does it ask Claude itself (`claude -p`) to re-derive them,
persisting the result per version so later runs reuse it.

## How the patch works

Every request Claude Code sends carries a `speed` field and a fast-mode beta
header, each gated on the caller having explicitly asked for fast mode — a flag the
delegated-agent spawn paths never set. cc-patch blanks that "explicitly asked"
requirement at both gates while leaving the model-eligibility check intact, so an
Opus delegated agent qualifies on its own. The edit is verified end to end: a
patched binary produces `usage.speed: fast` on an Opus subagent and leaves
sonnet/fable at `standard`.

Patches live in a registry (`internal/registry`); run `cc-patch list` to see them.

## Commands

| Command | What it does |
|---|---|
| `status` | Report whether each patch is applied. Read-only. |
| `apply --all` | Patch the installed binary and re-sign it. |
| `restore` | Restore the pristine, vendor-signed binary from backup. |
| `heal --all` | Re-apply, re-deriving through Claude when an update drifts a patch. |
| `list` | List the registered patches. |
| `install-daemons` | Install the watcher and daily heal launchd agents. |
| `uninstall-daemons` | Remove both agents. |

`--all` operates on every registered patch; `--id <patch>` targets one.

Status: works against Claude Code 2.1.217 on macOS (arm64). The patch is traced and
runtime-verified for that release; a future release that reshapes the code triggers
`heal`'s re-derivation.
