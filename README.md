# Statemate

Declarative system configuration management. Manage dotfiles, configs, and packages across machines.

## Features

- **Stow-style sources** - Organize configs by app, deployed relative to home
- **Profiles** - Machine-specific configs with auto-detection
- **Templates** - Go text/template for dynamic configs
- **Encryption** - Age encryption for sensitive files
- **Packages** - Declarative package management (brew, pacman, AUR)
- **Scripts** - Lifecycle scripts (before/after apply, run once, on change)

## Installation

```bash
# Homebrew (macOS/Linux)
brew install subbeh/tap/statemate

# Go install
go install github.com/subbeh/statemate/cmd/mate@latest

# From source
git clone https://github.com/subbeh/statemate
cd statemate && make build-all
```

## Quick Start

```bash
# Initialize (creates mate.yaml or mate.toml)
mate init

# Add source directories to config, then add files
mate add ~/.config/nvim/init.lua

# Preview changes
mate status
mate diff

# Apply
mate apply
```

## Configuration

```yaml
# mate.yaml
sources: [nvim, zsh, git]
target_base: "~"

profiles:
  base:
    sources: [common]
  work:
    extends: base
    detection:
      hostname: "work-*"
    variables:
      email: "work@example.com"
    packages:
      brew: [slack]

age:
  identity: "~/.config/statemate/key.txt"
  recipients: ["age1..."]

variables:
  email: "you@example.com"

var_files:
  - .matedata/secrets.yaml  # loaded relative to source dir

packages:
  brew: [ripgrep, fd]
```

## File Attributes

Use `#` suffixes to control behavior (stripped from target filename):

| Suffix | Description |
|--------|-------------|
| `#profile:name` | Only apply for specified profile |
| `#perm:600` | Set file permissions |
| `#owner:user` | Set file owner |
| `#group:group` | Set file group |
| `#perm-r:600` | Recursive permissions (applies to all children) |
| `#owner-r:user` | Recursive owner (applies to all children) |
| `#group-r:group` | Recursive group (applies to all children) |
| `#encrypted` | File is age-encrypted |
| `#template` | Process as Go template |
| `#symlink` | Create symlink instead of copy |

Example: `.ssh/config#encrypted#perm:600`

## Source Directory Config

Each source directory can have a `.mate.yaml` to configure behavior:

```yaml
# myapp/.mate.yaml
profile: linux              # only apply this source for linux profile
owner: root                 # default owner for all files
group: root                 # default group for all files
perm: "644"                 # default permissions for all files
targets:
  etc: /etc                 # map etc/ subdirectory to /etc instead of ~/etc
```

This is useful for system files:
```
arch_root/
  .mate.yaml                # targets: { etc: /etc }, owner: root
  etc/
    issue                   # deployed to /etc/issue as root
    keyd/
      default.conf          # deployed to /etc/keyd/default.conf as root
```

## Secrets

Secrets are referenced inline in templates using the `bitwarden` function. No separate config needed — just use them where you need them:

```
{{ bitwarden "github.com" "field" "gh-cli-token" }}
{{ bitwarden "work-ssh-key" "ssh" "private" }}
{{ bitwarden "gpg-keys" "attachment" "user@example.com.priv.asc" }}
{{ bitwarden "github.com" "login" "password" }}
{{ bitwarden "github.com" "totp" "" }}
```

**Syntax:** `{{ bitwarden "<item-name>" "<type>" "<field>" }}`

**Types:** `field` (custom field), `ssh` (private/public), `attachment` (base64-encoded), `login` (username/password/uri), `totp`

`mate secrets fetch` scans all templates to discover references, fetches from Bitwarden, and caches to an Age-encrypted file. Apply and diff read from cache.

```bash
mate secrets fetch             # fetch all discovered secrets
mate secrets fetch "github*"   # fetch matching item pattern
mate secrets list              # show cache status per secret
mate secrets status            # show secrets needing fetch
```

Cache is encrypted to local Age identity only. Optional config:
```yaml
secrets_cache: "~/.local/state/statemate/secrets.age"  # default location
```

## Scripts

Place scripts in `.matescripts/` at repo root or in source directories.

Format: `<order>-<name>.<ext>#<freq>#<timing>[#template]`

| Attribute | Values |
|-----------|--------|
| freq | `once`, `onchange`, `always` (omit for manual) |
| timing | `before`, `after` (default: before) |
| template | Add `#template` to render before execution |

Examples:
- `01-setup.sh#once#before` - run once before apply
- `02-secrets.sh#onchange#before#template` - run when changed, render first
- `99-cleanup.sh#always#after` - run after every apply

## Templates

Templates use Go's `text/template` syntax with these variables and functions:

**Variables:**
- `.Profile`, `.Hostname`, `.OS`, `.Arch`, `.HomeDir`, `.Username`
- `.SourceDir` - path to dotfiles directory
- `.Vars` - custom variables from config/var_files
- `.Env` - environment variables

**Functions:**
- `{{ bitwarden "item" "type" "field" }}` - fetch secret from cache (see Secrets)
- `{{ required .Vars.secret }}` - error if value is missing/empty
- `{{ default "fallback" .Vars.maybe }}` - use fallback if nil/empty
- `{{ env "HOME" }}` - get environment variable
- `{{ cmd "hostname -s" }}` - run command
- `{{ base64Decode "..." }}` - decode base64 string

## Local Config

Machine-specific overrides at `~/.config/statemate/mate.yaml`:

```yaml
source_dir: "~/dotfiles"  # where to find mate.yaml
profile: work             # override auto-detection
```

## Commands

```
mate init       Initialize a new dotfiles repo
mate apply      Apply configuration to targets
mate status     Show pending changes (--short for statusline)
mate diff       Show file differences
mate add        Add a file to management
mate edit       Edit a managed file
mate forget     Stop tracking a file
mate delete     Remove source and target file
mate encrypt    Encrypt a file in place
mate decrypt    Decrypt a file in place
mate rename     Rename a managed file
mate managed    List all managed files
mate profile    Show active profile
mate packages   Manage packages
mate secrets    Manage secrets (fetch, list, status)
mate scripts    List scripts
mate run        Run a script manually
mate doctor     Check configuration
```

## Shell Completions

```bash
mate completion bash > /etc/bash_completion.d/mate
mate completion zsh > "${fpath[1]}/_mate"
mate completion fish > ~/.config/fish/completions/mate.fish
```

## License

MIT
