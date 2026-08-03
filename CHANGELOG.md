# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.13.0] - 2026-08-03

### Changed

- cc-patch is macOS-only. Release builds and CI drop Linux, following daemonkit
  v0.21.0, which no longer compiles off macOS.
- Pin daemonkit v0.21.0. The watch and heal agents are applied and removed one
  label at a time through `daemonkit/launchd` instead of converging a service
  controller, so cc-patch no longer keeps its own daemon-state databases at
  `~/.local/share/cc-patch/daemon-services.db` and
  `daemon-service-processes.db`. Those two files are left behind by this
  upgrade and can be deleted by hand.
- `uninstall-daemons` attempts every label and reports the failures together,
  so one refusal no longer strands the remaining agent.
- The watch and heal agents still register the version-stable program path
  `~/.daemonkit/bin/cc-patch`. cc-patch now places that copy of its own
  executable and refreshes it by content digest, because daemonkit v0.21 places
  a program only for a daemon it serves. `uninstall-daemons` deletes the copy
  and the sidecar the previous release left beside it.

### Fixed

- `uninstall-daemons` removes the launchd plists written by earlier releases.
  daemonkit v0.21 proves ownership from a marker inside the plist and refuses
  one that carries none, so every agent from an earlier install would otherwise
  have stayed loaded.
- The launchd log directory resolves the user's home through the passwd
  database. Agents registered under Homebrew's post-install hook, which runs
  with a sandboxed temporary `HOME`, now write to the user's own
  `~/Library/Logs/cc-patch` instead of a directory that is deleted when the
  install finishes.

## [0.12.3] - 2026-07-27

### Changed

- Pin daemonkit v0.20.9 so the launchd agents resolve the real home from the
  passwd database: a helper installed under Homebrew postinstall's sandboxed
  temporary `HOME` registers in the user's own `~/Library/LaunchAgents` instead
  of a throwaway directory. `launchctl` exit 5 is no longer retried as
  transient, and recovery-mode reconcile clears an install wedged against its
  own stale registration.

## [0.12.2] - 2026-07-25

### Changed

- Pin daemonkit v0.20.6 so a launchd agent whose service was booted out reads
  as drift rather than a hard failure (`launchctl print` exit 113), letting
  `install-daemons` and `status` converge on a cold upgrade, and so durable
  untrack no longer spends a settling child's reap budget on the store.

## [0.12.1] - 2026-07-24

### Changed

- The watch and heal launchd agents register at daemonkit's version-stable
  program path (`~/.daemonkit/bin/cc-patch`); daemonkit v0.20.1 heals the
  stored registration stranded at a deleted Caskroom version directory.

### Fixed

- A legacy pre-v0.5.0 `state.json` now fails with an actionable error naming
  the file and the remedy instead of `json: unknown field "overrides"`.

## [0.11.1] - 2026-07-24

### Changed
- Pin daemonkit v0.19.1 so a terminalized worker claim tears down its owning
  runtime instead of leaving service management alive over a closed worker pool.
- Require release tags to be annotated and signed by the trusted fleet release
  key before publishing binaries or updating Homebrew.

## [0.11.0] - 2026-07-24

### Changed
- Pin daemonkit v0.18.0 so service-controller verifier work uses
  daemonkit-owned time and byte budgets rather than product worker settings.

## [0.10.0] - 2026-07-24

### Changed
- Pin daemonkit v0.17.4 so service shutdown settles accepted-session terminal
  acknowledgements before closing transport ownership.

## [0.9.0] - 2026-07-24

### Changed
- Pin daemonkit v0.17.2 for the exact service controller and process-ownership
  runtime shipped across the fleet.

## [0.8.0] - 2026-07-23

### Changed
- Pin daemonkit v0.16.0 so daemon launch inherits the direct-parent
  spawned-session ownership proof before descriptor transfer.

## [0.7.0] - 2026-07-23

### Changed
- Pin daemonkit v0.15.0 as the exact fleet runtime dependency.
- Daemon installation now converges one durable daemonkit service plan instead
  of managing launchd plists and lifecycle commands directly.

## [0.6.0] - 2026-07-23

### Changed
- Pin daemonkit v0.10.0 as the exact fleet runtime dependency.

## [0.5.0] - 2026-07-23

### Changed
- Pin daemonkit v0.9.0 for the fleet-wide exact runtime hard cut.
- `~/.local/share/cc-patch/state.json` is now one exact, fingerprinted schema-v1
  envelope. Present legacy, partial, extended, stale, or corrupt documents fail
  loudly instead of being repaired or treated as an empty store.
- State updates now use cross-process serialization and a same-directory,
  fsync-backed atomic replacement. Pack removal and its override pruning commit
  together, and structurally derived sites become durable before their patch
  changes the Claude binary.

## [0.3.0] - 2026-07-23

### Added
- **tasktools builtin.** `cc-patch install tasktools` restores the session task
  tools — `TaskCreate`/`TaskUpdate`/`TaskGet`/`TaskList` and the task-list prompt
  attachment — that a server-side statsig killswitch (`tengu_vellum_ash`) hides on
  some models, such as Fable sessions. The patch blanks the config name at its
  single read site so the killswitch is always off; `TodoWrite` is unaffected.

## [0.2.0] - 2026-07-23

### Added
- **Packs.** `cc-patch install <builtin>` installs a pack shipped in the binary by
  name; `cc-patch install <owner>/<repo>` clones a git repo carrying a
  `cc-patch/pack.toml`. `uninstall`/`update`/`list` manage them, and installed packs
  are covered by `apply --all`, `status`, and the launchd agents. The pack format is
  declarative — pinned sites, a Go RE2 `derive` DSL (named-capture `find`/`drop`,
  cross-site `bind` + `{{name}}` interpolation), and a heal prompt.
- Homebrew registers the launchd agents automatically on `brew install`/`brew
  upgrade`, so `install-daemons` is no longer a manual step.

### Changed
- The fastmode patch is now an opt-in builtin pack rather than always-applied.
  Install it with `cc-patch install fastmode`.

## [0.1.1]

### Fixed
- `apply` now persists structurally-derived sites per version, so `status` and
  later applies recognize a derived patch instead of reporting `drifted`. The
  derivation anchor is blanked by the patch itself and cannot be re-derived once
  applied, so without the persisted sites a derived patch looked unpatched.
- `status` falls through to derivation when the pinned literals are missing,
  matching what `apply` does.

## [0.1.0]

### Added
- `apply` / `restore` — patch the installed Claude Code binary so delegated Opus
  agents (subagents, teammates, workflow branches) run in fast mode, and roll back
  to the pristine vendor-signed binary. The edit is length-neutral and re-signed
  ad-hoc; only Opus-family agents are affected.
- `status` / `list` — report whether each registered patch is applied, and list
  the registered patches.
- `heal` — re-apply patches after a Claude Code update, re-deriving the patch
  sites structurally and, on deeper drift, through `claude -p`.
- `install-daemons` / `uninstall-daemons` — launchd agents that re-patch on
  auto-update (WatchPaths) and heal daily (StartCalendarInterval).

[0.13.0]: https://github.com/yasyf/cc-patch/compare/v0.12.3...v0.13.0
[0.12.3]: https://github.com/yasyf/cc-patch/compare/v0.12.2...v0.12.3
[0.12.2]: https://github.com/yasyf/cc-patch/compare/v0.12.1...v0.12.2
[0.12.1]: https://github.com/yasyf/cc-patch/compare/v0.11.1...v0.12.1
[0.11.1]: https://github.com/yasyf/cc-patch/compare/v0.11.0...v0.11.1
[0.11.0]: https://github.com/yasyf/cc-patch/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/yasyf/cc-patch/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/yasyf/cc-patch/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/yasyf/cc-patch/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/yasyf/cc-patch/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/yasyf/cc-patch/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/yasyf/cc-patch/compare/v0.4.0...v0.5.0
[0.3.0]: https://github.com/yasyf/cc-patch/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/yasyf/cc-patch/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/yasyf/cc-patch/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/yasyf/cc-patch/releases/tag/v0.1.0
