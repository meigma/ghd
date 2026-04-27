---
title: GitHub Downloader
slug: /
description: Documentation for installing GitHub release assets with ghd.
---

# GitHub Downloader

GitHub Downloader (`ghd`) is an experimental CLI for installing GitHub release
assets only after the selected artifact passes strict integrity and provenance
checks.

`ghd` is built for projects that publish binaries through GitHub Releases and
want consumers to verify more than a checksum. It checks the immutable GitHub
release record, SLSA provenance, and the GitHub Actions workflow identity before
it downloads or installs a release asset.

## Start Here

- [Get started with `ghd`](getting-started.md) walks through a first verified
  download and install.
- [Manage packages](manage-packages.md) covers the common package lifecycle:
  repository indexing, discovery, install, check, update, verify, and uninstall.
- [Security model](security-model.md) explains what `ghd` verifies and what it
  intentionally does not claim to solve.
- [Publisher guide](publisher-guide.md) explains how maintainers can publish
  GitHub releases that `ghd` can verify and install.
- [Reference](reference.md) lists command targets, flags, output modes, local
  paths, and `ghd.toml` fields.

## Current Status

`ghd` is preparing its first public release. The primary install path is the
Homebrew cask published from `meigma/homebrew-tap`:

```sh
brew install --cask meigma/tap/ghd
```

Manual binaries are also published through
[GitHub Releases](https://github.com/meigma/ghd/releases).

The current documentation reflects the implemented command surface and the live
first-party example release `meigma/ghd/ghd-example@1.1.1`.
