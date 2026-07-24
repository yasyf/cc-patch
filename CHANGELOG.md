# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/yasyf/cc-patch/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/yasyf/cc-patch/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/yasyf/cc-patch/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/yasyf/cc-patch/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/yasyf/cc-patch/compare/v0.4.0...v0.5.0
[0.3.0]: https://github.com/yasyf/cc-patch/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/yasyf/cc-patch/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/yasyf/cc-patch/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/yasyf/cc-patch/releases/tag/v0.1.0
