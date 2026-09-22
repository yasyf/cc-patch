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

Another builtin, **workflowdefault**, blanks the Workflow tool's explicit-opt-in
gate — the description text that forbids dynamic workflows unless the user typed
"ultracode" or asked for one in their own words — so a standing CLAUDE.md opt-in
governs, without ultracode's forced xhigh effort.

A third, **worktreeguard**, narrows the guard in a worktree-isolated session so
incidental mentions of git stop causing refusals and `~/...` paths reach its
existing repository checks.

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

## Check running sessions

A patch edits the binary file on disk. It reaches only processes that exec that
file after the edit. `apply` publishes the patched binary by rename, and an
already-running process keeps the old file mapped. A child forked from that
process inherits the old mapping too, even if it starts after the patch.

Run `cc-patch status` to check running sessions. After the per-patch lines, it
prints the binary's last-write time and the processes owned by your user that
exec through the installed Claude Code launcher. Each row shows a pid, start
time, and verdict. `current` means the process maps the binary now on disk;
`stale` means it maps a different file. File identity decides the verdict;
start times are context. For example:

```text
binary last written 2026-09-20 22:06:04; 22 claude processes running
  pid 30528  started 2026-09-21 19:40:43  current
  pid 69074  started 2026-09-20 19:17:20  stale
  ...
5 running processes would not name their executable, so a claude process may be missing above
17 of 22 running processes do not map this binary, so they do not have the patches above — restart Claude Code
```

Restart stale Claude Code sessions to load the patched binary. If a process
does not name its executable, `status` reports the count and warns that the list
may be incomplete. If it identifies a Claude Code process but cannot inspect
its mapping while it remains alive, the command fails and names the pid.

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
cc-patch install noshadow                  # another builtin
cc-patch install workflowdefault           # another builtin
cc-patch install worktreeguard             # and another
cc-patch install <owner>/<repo>[@<ref>]    # a remote pack: clone, validate, record
cc-patch install ./my-pack                 # a local pack: validate and link
cc-patch uninstall fastmode                # or <owner>/<repo>
cc-patch update [<owner>/<repo>]           # re-clone remotes; builtins track cc-patch
cc-patch list                              # installed patches + available builtins
```

A pack you are writing yourself is local. Every directory under
`~/.config/cc-patch/packs` is discovered on load as `local/<dir>`, so a local pack
needs no record in cc-patch's state and your edits take effect on the next load.
`cc-patch install <dir>` links a pack authored elsewhere into that directory;
deleting the link uninstalls it.

Each patch in a `pack.toml` is declarative. Pinned sites are the exact byte runs to
edit in the current release. A site either sets `drop`, blanking a substring of
`find` to spaces to neutralize a gate, or sets `replace`, substituting the whole run
to rewrite a value; either way the edit is length-neutral, so `replace` must match
`find` byte for byte in length.
A site can instead set `pool_find` and `pool_replace`, the old and new string of an
entry in the JavaScriptCore constant pool a Bun-compiled binary carries. That entry
holds a length and a precomputed hash alongside the characters, so cc-patch rewrites
all three and pads back out to the width the old string occupied, refusing a
replacement too wide for it. Reach for this when the JS source spelling a literal is
retained but dead: the pool is deduplicated, so its entry is what actually runs.
An optional derive adds Go RE2 patterns that re-locate the sites after an update
renames the minified locals. `find` selects a capture group by name or index, and
each site then takes either `drop`, another group to blank, or `replace`, a
template that renders the substitute from `{{group}}` references and must come out
the same length as `find`. `bind` exports a named capture, and `{{name}}` in a
later site's pattern pins it against an earlier site's exact match. For a site an
update cannot drift, `pinned = true` re-emits the pack's pinned literal at the same
position instead of matching a pattern; it takes no `pattern`, `find`, `drop`,
`replace` or `bind`. A derive must cover every site of its patch, because recovery
replaces the whole site list. An optional heal prompt lets cc-patch ask
Claude to re-locate the sites when even the derive drifts.

See [`internal/builtins/packs/fastmode/pack.toml`](internal/builtins/packs/fastmode/pack.toml)
for a worked example.

## How the fastmode patch works

Every request Claude Code sends carries a `speed` field and a fast-mode beta
header, each gated on the caller having explicitly asked for fast mode, a flag the
delegated-agent spawn paths never set. The fastmode pack blanks that "explicitly
asked" requirement at both gates while leaving the model-eligibility check intact,
so an Opus delegated agent qualifies on its own. The edit is verified end to end: a
patched binary produces `usage.speed: fast` on an Opus subagent and leaves
sonnet/fable at `standard`.

## How the noshadow patch works

Claude Code injects `find()` and `grep()` wrappers into every Bash shell, and
those wrappers exec the embedded `bfs` and `ugrep`. The embedded `ugrep` can
busy-poll stdin in `select()` and keep burning 100% CPU long after the tool call
that spawned it has finished ([anthropics/claude-code#69736](https://github.com/anthropics/claude-code/issues/69736)).

The wrapper already falls back to the system tool when its embedded binary is
missing: `if [[ ! -x $_cc_bin ]]; then command <tool> …; return; fi`. The
noshadow pack blanks the `! -x` test, leaving `[[ $_cc_bin ]]` — always true for
a resolved path — so every call takes the vendor's own fallback. The tool name is
interpolated, so one edit covers both `find` and `grep`.

Two copies of that wrapper live in the bundle. The bytecode runs against a
deduplicated constant-pool entry, so noshadow rewrites that entry as a pool site
and recomputes the hash the pool precomputes for it; a second site edits the
retained JavaScript template source.

## How the workflowdefault patch works

The Workflow tool's description opens with a hard gate: only an explicit
per-session opt-in (the "ultracode" keyword, an ultracode session, or a request in
the user's own words) may trigger a dynamic workflow, and a task that would merely
benefit from one does not count — which overrides a CLAUDE.md standing opt-in. The
pack blanks that gating block to spaces (length-neutral), along with the two
places that point back at it: the Ultracode paragraph's fallback sentence, and the
ultracode-off system-reminder's claim that the gate applies again. Nothing in the
tool description then contradicts the user's own instructions, and CLAUDE.md
decides when workflows run.

## How the worktreeguard patch works

Claude Code checks shell commands in a worktree-isolated session before running
them. The command guard's call site tests only whether the shell is bash, so even
a loop over `gh pr view` can draw a refusal about git. The
`git-only-command-guard` patch tests the raw command string for `git` instead,
paying for the extra bytes by shortening `:null` to `:0`. Commands that match
still enter the guard; the cwd-escape guard immediately before it still runs.

Inside the command guard, `drop-raw-substring-branch` disables the branch that
refuses a compound command because its text contains `git`. A loop, chain or
heredoc then reaches the existing structural walk, which models directory changes
and redirects from the parse tree. An aborted parse with no tree to walk still
refuses.

The `drop-interpreter-payload-check` patch removes the check for a whole `git`
token in text handed to a non-shell interpreter. Prose and comments in a Python
heredoc no longer trigger it. This also permits an interpreter payload to invoke
git against another worktree; the guard cannot parse that payload. Text handed to
`sh` or `bash` still goes through shell analysis.

The `expand-leading-tilde` patch expands a leading `~/` using `process.env.HOME`
in the resolver shared by `cd`, `env -C`, git directory arguments and assignment
values. The resolver otherwise treats every tilde as opaque and refuses `~/...`
as a path computed at runtime. For unquoted `~/...` operands, expansion lets the
existing repository checks judge the resolved directory: an unrelated repository
is allowed, while the shared checkout still refuses, now by its own name. Quoted
`~/...` operands can bypass that refusal if a directory or symlink literally
named `~` in the session's cwd leads to the shared checkout: the guard checks
`$HOME/...`, while the command reaches `<cwd>/~/...`. Only `~/` expands. `~user`
and a bare `~` stay opaque, and an unset HOME leaves the tilde in place so the
path still refuses. The edit fits in the resolver's original 197 bytes.

## Commands

| Command | What it does |
|---|---|
| `install <owner>/<repo> \| <builtin> \| <dir>` | Install a remote pack or builtin, or link a local pack directory. |
| `uninstall <owner>/<repo> \| <builtin>` | Remove an installed pack and its state. |
| `update [<owner>/<repo>]` | Re-clone a remote pack, or all remotes. |
| `list` | List installed patches and available builtins. |
| `status` | Report applied patches and whether running Claude Code processes map the current binary. Read-only. |
| `apply --all` | Patch the installed binary and re-sign it. Carries on past a patch that fails and exits non-zero. |
| `restore` | Restore the pristine, vendor-signed binary from backup. |
| `heal --all` | Re-apply, re-deriving through Claude when an update drifts a patch. Carries on past a patch that fails and exits non-zero. |
| `install-daemons` | Install the watcher and daily heal launchd agents. |
| `uninstall-daemons` | Remove both agents. |

`--all` operates on every installed patch; `--id <namespace>/<patch>` targets one
(e.g. `fastmode/delegated-agents`, `workflowdefault/workflow-optin`, or
`<owner>/<repo>/<patch>`).

Status: works on macOS (arm64). The engine is release-agnostic; a pack's pinned
sites are version-proven, and a release that reshapes the code triggers the pack's
`derive` and then `heal`.
