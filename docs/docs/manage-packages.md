---
title: Manage Packages
description: Common ghd workflows for package discovery, install, update, verification, and removal.
---

# Manage Packages

This guide covers the common package lifecycle. Use `--non-interactive` when
you want plain output and no transient terminal UI. Add `--json` on supported
read-only or result-oriented commands when a script should parse the result.

## Add and Refresh Repositories

Add a repository to the local index:

```sh
ghd repo add owner/repo
```

Refresh one indexed repository:

```sh
ghd repo refresh owner/repo
```

Refresh every indexed repository:

```sh
ghd repo refresh --all
```

List indexed repositories:

```sh
ghd repo list
```

Remove a repository from the index:

```sh
ghd repo remove owner/repo
```

`repo remove` only updates the local index. It does not uninstall packages that
were already installed from that repository.

## Discover Packages

List packages from the local index:

```sh
ghd list
```

List packages directly from one repository without adding it to the index:

```sh
ghd list owner/repo
```

Show package details:

```sh
ghd info package
ghd info owner/repo
ghd info owner/repo/package
```

`ghd info owner/repo` auto-selects the package only when the repository declares
exactly one package.

## Install Packages

Install an indexed package by name:

```sh
ghd --yes --non-interactive install package
```

Install a specific version:

```sh
ghd --yes --non-interactive install package@1.2.3
```

Install directly from a repository without relying on the local index:

```sh
ghd --yes --non-interactive install owner/repo/package
ghd --yes --non-interactive install owner/repo/package@1.2.3
```

If `@version` is omitted, `ghd` resolves the latest eligible stable release for
the package on the current platform. Prereleases require an explicit version.

## Check, Update, and Verify

Check one package for updates:

```sh
ghd check package
```

Check all installed packages:

```sh
ghd check --all
```

Update one package:

```sh
ghd --yes --non-interactive update package
```

Update all installed packages:

```sh
ghd --yes --non-interactive update --all
```

Re-verify one installed package:

```sh
ghd verify package
```

Re-verify all installed packages:

```sh
ghd verify --all
```

Updates use the same verification path as installs. If an update would change
the trusted signer workflow, ordinary `--yes --non-interactive` is not enough;
review it interactively or approve the signer change explicitly:

```sh
ghd --yes --approve-signer-change --non-interactive update package
```

## Inspect Local State

List installed packages:

```sh
ghd installed
```

Check environment readiness:

```sh
ghd doctor
```

`doctor` checks the local index, store, state, binary directory, GitHub API
connectivity, and whether the managed binary directory is on `PATH`.

## Uninstall Packages

Uninstall by package name:

```sh
ghd uninstall package
```

Uninstall by fully qualified target:

```sh
ghd uninstall owner/repo/package
```

Uninstall removes the active managed package state, store contents, and exposed
binary links for that package.
