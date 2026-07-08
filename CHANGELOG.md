# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Per-source `target_base` in `.mate.yaml` to deploy files to a different root directory
- `.mate.yaml` files now support template rendering (use variables like `{{ .Vars.workspace }}`)
- `mate add` prompts to create/update `.mate.yaml` when adding files outside home directory
- `mate packages apply` now shows package manager output during install/uninstall
- `generate` directive in `.mate.yaml` to dynamically create files from templates
- `indent` template function for proper YAML multiline content formatting

### Changed
- `mate profile` now shows the sources that apply to the active profile

## [0.1.0] - 2026-07-07

Initial release.

### Features
- Stow-style multi-directory source management
- Profile-based configuration with auto-detection (hostname, OS, arch, user, command)
- Profile inheritance via `extends`
- Go text/template support for dynamic configuration
- Age encryption for sensitive files
- Declarative package management (brew, pacman, AUR)
- Lifecycle scripts with frequency control (once, always, onchange)
- File attributes via `#` suffixes (profile, perm, owner, group, encrypted, template, symlink)
- Secrets management with Bitwarden integration and age-encrypted cache
- `.mateignore` file support for excluding files
- Source-level configuration via `.mate.yaml`
- Shell completions (bash, zsh, fish, powershell)

### Commands
- `init` - Initialize a new statemate repository
- `apply` - Apply configuration to target
- `status` - Show pending changes
- `diff` - Show full unified diff
- `check` - Validate configuration
- `add` - Add file to source
- `forget` - Remove file from tracking
- `delete` - Delete file from source and target
- `rename` - Rename managed file
- `encrypt` - Encrypt a file in place
- `decrypt` - Decrypt a file in place
- `edit` - Edit file (with auto decrypt/encrypt)
- `eval` - Render template to stdout
- `cat` - Display file contents (with auto decrypt)
- `clean` - Remove orphaned files
- `managed` - List managed files
- `profile` - Show active profile
- `packages` - Manage system packages
- `scripts` - List and run scripts
- `secrets` - Manage secrets cache
- `doctor` - Check system health
