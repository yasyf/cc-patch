# cc-patch

Patch the installed Claude Code binary, and keep the patch applied across every
update.

cc-patch is a patch engine. It applies length-neutral byte edits to the Claude
Code binary, re-signs it, and re-applies itself whenever an auto-update replaces
the binary. It applies no patch until you install one: each patch arrives as a
**pack**, either a builtin shipped with cc-patch (installed by name) or a git repo
you install by `<owner>/<repo>`.

The flagship builtin is **fastmode**. Fast mode (priority-tier Opus) applies to
your top-level Claude Code session, while the agents it spawns run at standard
speed: subagents, agent-team teammates, and workflow branches. The fastmode pack
patches the binary so delegated Opus agents run fast too.

[![CI](https://img.shields.io/github/actions/workflow/status/yasyf/cc-patch/ci.yml?branch=main&label=ci)](https://github.com/yasyf/cc-patch/actions/workflows/ci.yml)

## Get started

```bash
brew install yasyf/tap/cc-patch
cc-patch install fastmode   # install the builtin fastmode pack

cc-patch status   # is the installed Claude Code binary patched?
```

`status` reads `~/.local/bin/claude`, resolves it to the versioned binary, and
reports whether each installed pack's patches are applied. It never writes.

<details>
<summary>From a clone</summary>

```bash
git clone https://github.com/yasyf/cc-patch
cd cc-patch
task build   # -> ./bin/cc-patch
```

</details>

## Apply the patch

```bash
cc-patch apply --all       # patch the current binary and re-sign it
cc-patch restore           # roll back to the pristine, vendor-signed binary
```

`apply` edits the binary in place (a length-neutral byte overwrite, no segment
resize), backs the original up to `<binary>.ccpatch-orig`, and re-signs the result
ad-hoc. It is idempotent: a second `apply` reports `already patched`.

The fastmode pack keeps Claude Code's own model-eligibility gate, so only
Opus-family delegated agents are affected. Sonnet and Fable agents still run at
standard speed and the server never sees a fast request it would reject.

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

`brew install` and `brew upgrade` register these agents for you; the commands above
are for a manual install or to remove them. The watcher fires whenever
`~/.local/share/claude/versions` or the launcher changes, and runs `cc-patch apply
--all` against the new binary. The heal job runs `cc-patch heal --all` once a day.

`heal` re-applies each patch, escalating only when an update has shifted the code:
first it re-locates the sites structurally (tolerating renamed locals), and only if
that fails does it ask Claude itself (`claude -p`) to re-derive them, persisting the
result per version so later runs reuse it.

## Packs

A pack carries binary patches at the well-known path `cc-patch/pack.toml`.
Builtins ship inside cc-patch and install by name; remote packs are git repos you
install by `<owner>/<repo>`. Installing one is the opt-in act, so installed packs
are covered by `apply --all` and the daemons.

```bash
cc-patch install fastmode                  # a builtin, by name
cc-patch install <owner>/<repo>[@<ref>]    # a remote pack: clone, validate, record
cc-patch uninstall fastmode                # or <owner>/<repo>
cc-patch update [<owner>/<repo>]           # re-clone remotes; builtins track cc-patch
cc-patch list                              # installed patches + available builtins
```

Each patch in a `pack.toml` is declarative. Pinned sites are the exact byte runs to
blank in the current release, each a length-neutral edit that neutralizes one gate.
An optional derive adds Go RE2 patterns that re-locate the sites after an update
renames the minified locals: `find` and `drop` select a capture group by name or
index, `bind` exports a named capture, and `{{name}}` in a later site's pattern pins
it against an earlier site's exact match. An optional heal prompt lets cc-patch ask
Claude to re-locate the sites when even the derive drifts.

See [`internal/builtins/packs/fastmode/pack.toml`](internal/builtins/packs/fastmode/pack.toml)
for a worked example, including the cross-site pin.

## How the fastmode patch works

Every request Claude Code sends carries a `speed` field and a fast-mode beta
header, each gated on the caller having explicitly asked for fast mode, a flag the
delegated-agent spawn paths never set. The fastmode pack blanks that "explicitly
asked" requirement at both gates while leaving the model-eligibility check intact,
so an Opus delegated agent qualifies on its own. The edit is verified end to end: a
patched binary produces `usage.speed: fast` on an Opus subagent and leaves
sonnet/fable at `standard`.

## Commands

| Command | What it does |
|---|---|
| `install <owner>/<repo> \| <builtin>` | Install a remote pack or a builtin. |
| `uninstall <owner>/<repo> \| <builtin>` | Remove an installed pack and its state. |
| `update [<owner>/<repo>]` | Re-clone a remote pack, or all remotes. |
| `list` | List installed patches and available builtins. |
| `status` | Report whether each patch is applied. Read-only. |
| `apply --all` | Patch the installed binary and re-sign it. |
| `restore` | Restore the pristine, vendor-signed binary from backup. |
| `heal --all` | Re-apply, re-deriving through Claude when an update drifts a patch. |
| `install-daemons` | Install the watcher and daily heal launchd agents. |
| `uninstall-daemons` | Remove both agents. |

`--all` operates on every installed patch; `--id <namespace>/<patch>` targets one
(e.g. `fastmode/delegated-agents` or `<owner>/<repo>/<patch>`).

Status: works on macOS (arm64). The engine is release-agnostic; a pack's pinned
sites are version-proven, and a release that reshapes the code triggers the pack's
`derive` and then `heal`.
