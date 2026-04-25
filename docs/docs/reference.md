---
title: Reference
description: ghd command, target, output, path, and ghd.toml reference.
---

# Reference

## Target Syntax

`ghd` accepts package targets in a few forms depending on the command:

| Form | Meaning |
| --- | --- |
| `package` | Resolve a package or binary name through the local index or installed state. |
| `package@version` | Resolve an indexed package at a specific version. |
| `owner/repo` | Refer to a GitHub repository for repository discovery commands. |
| `owner/repo/package` | Refer to one package in one GitHub repository. |
| `owner/repo/package@version` | Refer to one package version in one GitHub repository. |

`info` does not accept `@version`; it is a discovery command for package
metadata, not release-specific verification.

## Commands

| Command | Purpose |
| --- | --- |
| `ghd download owner/repo/package@version --output DIR` | Download and verify one release asset without installing it. |
| `ghd repo add owner/repo` | Add a repository to the local index. |
| `ghd repo list` | List indexed repositories. Supports `--json`. |
| `ghd repo refresh [owner/repo \| --all]` | Refresh indexed repository manifests. |
| `ghd repo remove owner/repo` | Remove a repository from the local index. |
| `ghd list [owner/repo]` | List packages from the index or one repository. Supports `--json`. |
| `ghd info name\|owner/repo\|owner/repo/package` | Show package discovery details. Supports `--json`. |
| `ghd install package[@version]` | Verify and install a package resolved through the index. |
| `ghd install owner/repo/package[@version]` | Verify and install a package from a specific repository. |
| `ghd installed` | List installed packages. Supports `--json`. |
| `ghd check [name\|owner/repo/package\|--all]` | Check installed packages for updates. Supports `--json`. |
| `ghd update [name\|owner/repo/package\|--all]` | Verify and update installed packages. Supports `--json`. |
| `ghd verify [name\|owner/repo/package\|--all]` | Re-verify installed packages and managed binaries. Supports `--json`. |
| `ghd uninstall name\|owner/repo/package` | Uninstall one active package. |
| `ghd doctor` | Check local environment readiness. Supports `--json`. |

## Global Flags

| Flag | Meaning |
| --- | --- |
| `--github-api-url` | Override the GitHub REST API base URL. |
| `--index-dir` | Override the local repository index directory. |
| `--state-dir` | Override the local installed package state directory. |
| `--trusted-root` | Use a specific Sigstore `trusted_root.json`. |
| `--non-interactive` | Disable prompts, colors, and transient terminal UI. |
| `--yes` | Approve verified install actions and ordinary verified updates without prompting. |

Command-local flags include:

| Flag | Commands | Meaning |
| --- | --- | --- |
| `--output`, `-o` | `download` | Directory for the downloaded artifact and `verification.json`. |
| `--store-dir` | `install`, `update`, `uninstall`, `doctor` | Managed package store directory. |
| `--bin-dir` | `install`, `update`, `uninstall`, `doctor` | Managed binary link directory. |
| `--all` | `check`, `update`, `verify`, `repo refresh` | Operate on every relevant package or repository. |
| `--json` | `list`, `info`, `installed`, `check`, `verify`, `update`, `doctor`, `repo list` | Emit structured JSON result output. |
| `--approve-signer-change` | `update` | Allow an update to rotate the trusted release signer when combined with `--yes` for non-interactive approval. |

## Default Local Paths

On Unix-like systems, unset paths default to:

```text
$HOME/.local/share/ghd/index
$HOME/.local/share/ghd/store
$HOME/.local/state/ghd
$HOME/.local/bin
```

The managed binary directory must be on `PATH` for installed commands to be
available by name.

## Output Modes

Human terminal output is richer when stdout or stderr is a terminal. Use
`--non-interactive` for stable plain text and use `--json` where a structured
result contract exists.

The standalone `download` command writes stable stdout lines only in the plain
automation path:

```text
artifact PATH
verification PATH
```

The `install` command writes stable binary lines only in non-interactive mode:

```text
binary PATH
```

## `ghd.toml`

A compatible repository exposes a root `ghd.toml` manifest:

```toml
version = 1

[provenance]
signer_workflow = "owner/repo/.github/workflows/release.yml"

[[packages]]
name = "foo"
description = "Foo CLI"
tag_pattern = "foo-v${version}"

[[packages.assets]]
os = "darwin"
arch = "arm64"
pattern = "foo_${version}_darwin_arm64.tar.gz"

[[packages.assets]]
os = "linux"
arch = "amd64"
pattern = "foo_${version}_linux_amd64.tar.gz"

[[packages.binaries]]
path = "bin/foo"
```

Rules enforced by the current implementation:

- `version` must be `1`.
- `provenance.signer_workflow` must identify a GitHub Actions workflow path.
- at least one package must be declared.
- package names may contain letters, digits, `.`, `_`, and `-`.
- package names are unique case-insensitively within a manifest.
- `tag_pattern` defaults to `v${version}` when omitted.
- `tag_pattern` and asset `pattern` values must contain exactly one
  `${version}` token.
- binary paths are relative paths inside the verified asset or extracted archive.
- binary paths must not be absolute and must not contain `..`.
- assets are matched by Go-style `os` and `arch` values.

For install, download, check, and update trust decisions, the selected release
tag must contain a root `ghd.toml`. The default-branch manifest may help
discover a candidate tag, but release-tag metadata defines signer workflow,
asset names, and binary paths.
