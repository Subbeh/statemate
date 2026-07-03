# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `mate init` now creates a README.md with setup instructions for new machines
- `mate init` now initializes a git repository if not already in one
- `mate status` now shows source directory column with header
- `.mateignore` file support for excluding files from management (gitignore syntax)
- New script naming format: `<order>-<name>.<ext>#<freq>#<timing>[#template]`
  - Example: `01-setup.sh#once#before`, `02-cleanup.sh#always#after#template`
  - Frequency: `once`, `onchange`, `always` (omit for manual scripts)
  - Timing: `before`, `after` (defaults to `before`)
  - `#template` attribute for scripts that need template rendering
  - Extension comes before attributes (consistent with file naming)
- `default_source` config option to set default source for `mate add`
- `mate add` now checks if file is already managed before adding
- `mate add` interactive source selection with fuzzy search (when no default/flag)
- Tab completion for commands: forget, delete, diff, edit, encrypt, decrypt, rename, run
- Tab completion for flags: `--source`, `--profile`
- `mate status --short` flag for compact statusline output (format: `+N ~N !N ?N`)
- Source-level `owner`, `group`, and `perm` defaults in `.mate.yaml`
- Recursive directory attributes: `#owner-r:x`, `#group-r:x`, `#perm-r:x` (inherited by children)
- `.SourceDir` template variable for accessing the dotfiles source directory
- `required` template function to error on missing/empty values
- `var_files` config option to load additional variable files (e.g., secrets)

### Changed
- Empty sources list is now valid (allows orphan detection with no active sources)
- Script naming format changed from `run_<trigger>_<order>-<name>` to attribute-based format
- `mate remove` renamed to `mate delete`
- `mate profile` command to show active profile and detection source
- `STATEMATE_DIR` environment variable to specify dotfiles location
- Local machine config at `~/.config/statemate/mate.yaml` with same structure as repo config
- Local config overrides repo config (keys present in local config replace repo values)
- `mate init` now prompts for yaml or toml format (or use `--format`)
- `mate init` prompts to register the directory as dotfiles location
- `mate init` now handles cloned repos with existing configs (registers location instead of failing)
- Directory-level profile in `.mate.yaml` (e.g., `profile: linux`)
- Profile-level `sources` field - profiles can define additional source directories
- Sources from extended profiles are inherited (e.g., `work` extending `base` includes `base`'s sources)
- `mate managed [path]` command to list all managed files with their attributes
- `mate status` now warns about orphaned files (targets no longer in source)
- `mate rename` command to rename managed files (renames both source and target)
- `mate encrypt` command to encrypt existing files in place
- `mate decrypt` command to decrypt encrypted files in place
- `mate edit` command to edit files (decrypts/re-encrypts automatically for encrypted files)
- `mate apply` conflict prompt now has `[i]mport` option to copy target changes back to source
- `mate diff` now decrypts encrypted files before comparing
- `mate diff` now renders templates before comparing (shows actual diff, not template syntax)
- Colorized diff output for `mate diff` and `mate apply` conflict prompts
- `user` detection criteria for profile matching (matches `$USER` or `$USERNAME`)
- `command` detection criteria (renamed from `script` for clarity)
- `mate packages status --all` flag to show extra packages not in config
- `mate packages apply --prune` flag to remove packages not in config (with warning)
- `aur_helper` config option to specify AUR helper (default: yay, supports paru etc.)
- `common` package list for cross-platform packages (installed via brew or pacman)
- Package files can be split out to `.mate/packages.yaml` (auto-loaded if present)
- `package_files` config option to specify additional package files
- Source directories can define packages in `.mate/packages.yaml` (e.g., `./hyprland/.mate/packages.yaml`)
- Cross-platform builds (macOS arm64, Linux amd64) via `make build-all`

### Changed
- Profile detection now prioritizes child profiles over parents (inheritance depth sorting)
- Package management: `yay` list renamed to `aur` (works with any AUR helper)
- `brew` package list now uses `brew leaves --installed-on-request` (explicit packages only, not dependencies)
- `mate packages status` message clarifies "No packages configured" vs "No package managers available"
- Config and template context reloaded after before scripts run (allows scripts to generate var_files)
- `var_files` paths resolved relative to source directory (not working directory)

### Fixed
- `mate status` and `mate apply` no longer error when target file is missing
- `mate diff` now uses profile-resolved sources (was missing profile-level sources)
- Missing target files are now shown as modified (`~`) instead of conflict (`!`)
- Profile detection no longer randomly selects between parent and child profiles
- `mate profile` now shows detection reason correctly for all criteria
- `mate apply` conflict detection now compares rendered content for encrypted/template files
- `mate apply` now updates state DB for files where content matches but state was missing
- `mate diff` now properly handles files that are both encrypted and templated
- Diff shows "(no differences)" instead of empty output when files are identical
- Encrypted files without decryption key now show clear error instead of diffing gibberish

- Initial release
- Stow-style multi-directory source management
- Profile-based configuration with auto-detection (hostname, OS, arch, custom script)
- Profile inheritance via `extends`
- Go text/template support for dynamic configuration
- Age encryption for sensitive files
- Declarative package management (brew, pacman, yay)
- Lifecycle scripts (run_once, run_before, run_after, run_always, run_onchange)
- File attributes via `#` suffixes (profile, perm, owner, group, encrypted, template, symlink)
- Commands: init, apply, status, diff, check, add, forget, delete, rename, encrypt, decrypt, managed, profile, packages, scripts, run, doctor, version
- Shell completions (bash, zsh, fish, powershell)
- Man page and markdown documentation generation
