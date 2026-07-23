# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/yasyf/cc-patch/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/yasyf/cc-patch/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/yasyf/cc-patch/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/yasyf/cc-patch/releases/tag/v0.1.0
