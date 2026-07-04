# Secrets Management Spec

## Overview

Native secrets management for statemate, replacing the current shell script approach. Secrets are fetched from external providers (Bitwarden, generic commands), cached to disk with Age encryption, and made available to templates via a `.Secrets` namespace.

## Declaration

Secrets are declared in a provider-grouped structure in `mate.yaml`:

```yaml
secrets:
  cache: ~/.local/state/statemate/secrets.age  # optional, default: XDG state dir
  bitwarden:
    items:
      - path: age.key
        item: chezmoi
        type: field
        field: age-key
      - path: ssh.work.private
        item: work
        type: ssh
        field: private
      - path: ssh.work.public
        item: work
        type: ssh
        field: public
      - path: gpg.priv_asc
        item: gpg-keys
        type: attachment
        filename: user@example.com.priv.asc
      - path: github.token
        item: github.com
        type: login
        field: password
      - path: github.totp
        item: github.com
        type: totp
  command:
    items:
      - path: aws.session_token
        cmd: "aws sts get-session-token | jq -r .Credentials.SessionToken"
```

### Provider: Bitwarden

Supported item types:
- `field` - Custom field value on an item. Requires `field` (field name).
- `ssh` - SSH key item. Requires `field` (`private` or `public`).
- `attachment` - File attachment, stored base64-encoded. Requires `filename`.
- `login` - Login credentials. Requires `field` (`username`, `password`, or `uri`).
- `totp` - TOTP code generation. No extra fields needed.

### Provider: Command

Runs an arbitrary shell command. Output is trimmed and stored as a string value.

```yaml
command:
  items:
    - path: my.secret
      cmd: "pass show path/to/secret"
```

### Per-Source Secrets

Source directories can declare additional secrets in their `.mate.yaml`:

```yaml
# source/.mate.yaml
secrets:
  bitwarden:
    items:
      - path: app.api_key
        item: my-app
        type: field
        field: api-key
```

### Profile Secrets

Profiles can declare additional secrets (additive merge with global):

```yaml
profiles:
  work:
    secrets:
      bitwarden:
        items:
          - path: jira.api_token
            item: atlassian.com
            type: field
            field: api-key
```

If a profile declares the same `path` as a global secret, the profile's version overrides.

## Template Access

Secrets are available under `.Secrets` with nested dot-path access:

```
{{ .Secrets.ssh.work.private }}
{{ .Secrets.github.token }}
{{ required .Secrets.age.key }}
```

The dot-separated `path` field in the declaration creates nested maps:
- `path: ssh.work.private` -> `.Secrets.ssh.work.private`

Binary data (attachments) is always base64-encoded. Templates can use it as-is or decode:

```
{{ .Secrets.gpg.priv_asc | base64Decode }}
```

## Caching

### Storage

- Secrets are cached to an Age-encrypted file on disk.
- Default location: `~/.local/state/statemate/secrets.age`
- Configurable via `secrets.cache` in mate.yaml.
- Encrypted using the local machine's Age identity only (recipient derived from identity key, NOT the shared recipients list).

### Cache Format

The decrypted cache is JSON with metadata:

```json
{
  "fetched_at": "2026-07-01T12:00:00Z",
  "items": {
    "age.key": {"value": "AGE-SECRET-KEY-...", "fetched_at": "2026-07-01T12:00:00Z"},
    "ssh.work.private": {"value": "-----BEGIN...", "fetched_at": "2026-07-01T12:00:00Z"},
    "gpg.priv_asc": {"value": "base64data...", "fetched_at": "2026-07-01T12:00:00Z"}
  }
}
```

### Refresh Triggers

Secrets are fetched from providers only when:
1. `mate secrets fetch` is run explicitly.
2. A new secret is added or an existing declaration is modified in the config (detected at apply time; user is prompted).

On `mate apply`, cached values are used. If the cache is missing entirely, the user is prompted to run `mate secrets fetch`.

## Failure Handling

When a provider fails (vault locked, network error, CLI missing):
1. The fetch operation fails with a clear error message.
2. The user is prompted: "Fetch failed. Continue with cached secrets? [y/n]"
3. If yes and cache exists, apply proceeds with cached values.
4. If no cache exists, apply aborts.

## Bitwarden Integration

### CLI Interaction

Mate shells out to the `bw` CLI binary. On fetch:

1. Check vault status via `bw status`.
2. If locked, attempt unlock:
   - Try biometric/PIN first (`bw unlock` with biometric if configured).
   - Fall back to prompting for master password (secure terminal input).
   - Store BW_SESSION for the duration of the fetch.
3. Run `bw sync` to ensure fresh data.
4. Fetch items via `bw list items` (batch) or `bw get` (individual).
5. Extract values based on type.

### TOTP

TOTP codes are generated via `bw get totp <item-id>`. Note: these are ephemeral and shouldn't be cached long-term.

## CLI Commands

### `mate secrets fetch [pattern]`

Fetch secrets from providers and update the cache.

- With no arguments: fetches all declared secrets.
- With a glob pattern: fetches only matching paths (e.g., `ssh.*`, `github.*`).
- Interactive progress output showing each secret being fetched.
- Shows summary at end: "Fetched 12 secrets (3 changed, 9 unchanged)".

### `mate secrets list`

Display declared secrets with cache status:

```
PATH                  LAST FETCHED         STATUS
age.key               2026-06-30 14:22     cached
ssh.work.private      2026-06-30 14:22     cached
ssh.work.public       2026-06-30 14:22     cached
github.token          2026-06-30 14:22     cached
jira.api_token        -                    missing
new.secret            -                    new
```

Status values: `cached`, `missing` (never fetched), `new` (declaration added since last fetch).

### `mate secrets status`

Show secrets that need attention, including dependent files:

```
Secrets needing refresh:
  jira.api_token (missing - never fetched)
  new.secret (new declaration)

Affected files:
  ~/.config/jira/config.yaml (template, uses: jira.api_token)
  ~/.ssh/config (template, uses: new.secret)
```

## Status Integration

### `mate status` (normal)

When secrets are stale/missing, show a section:

```
Secrets:
  2 secrets need refresh (run 'mate secrets fetch')
```

### `mate status --short`

Add `sN` indicator for secrets needing refresh:

```
+2 ~1 *3 s2
```

Where `s2` means 2 secrets need fetching.

### Change Detection

When secrets have been refreshed and their values differ from what was used in the last apply, `mate status` should show dependent template files as modified. This is detected by comparing the hash of rendered output with current secrets vs. the stored applied hash.

Secret changes do NOT trigger `onchange` scripts.

## Doctor Integration

`mate doctor` validates secrets configuration:

- Config syntax is valid (paths, types, required fields).
- Provider CLIs are installed (`bw` binary exists in PATH).
- Vault auth state (is BW logged in? vault unlocked?).
- Age identity file exists and is readable (for cache encryption).
- Cache file exists and is decryptable.

## Provider Architecture

Providers implement an interface:

```go
type SecretProvider interface {
    Name() string
    Available() error           // check if provider CLI/deps exist
    Fetch(items []SecretItem) (map[string]string, error)
}

type SecretItem struct {
    Path     string
    Item     string  // provider-specific item identifier
    Type     string  // field, ssh, attachment, login, totp
    Field    string  // sub-field within the item
    Filename string  // for attachments
    Cmd      string  // for command provider
}
```

This allows adding future providers (1Password, pass/gopass, HashiCorp Vault) by implementing the interface.

## Migration

The current `01-fetch-secrets.sh` script writes to `.matedata/secrets.json` which is loaded via `var_files`. Migration path:

1. Add `secrets` block to mate.yaml translating the current SECRETS array.
2. Run `mate secrets fetch` to populate the Age-encrypted cache.
3. Update templates: change `.Vars.chezmoi.age_key` to `.Secrets.age.key`, etc.
4. Remove the script and `var_files` reference.
5. Remove `.matedata/secrets.json`.
