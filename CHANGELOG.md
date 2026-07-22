# Changelog

All notable changes to this project are documented here.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/yasyf/cc-patch/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/yasyf/cc-patch/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/yasyf/cc-patch/releases/tag/v0.1.0
