---
title: Get Started
description: Learn the first verified download and install workflow with ghd.
---

# Get Started

This tutorial uses `meigma/ghd/ghd-example@1.1.1`, the first-party example
release published from this repository. It is a small direct-binary package
used to validate `ghd` against real GitHub releases, release attestations, and
SLSA provenance.

## Install `ghd`

On macOS, install `ghd` from the Homebrew tap:

```sh
brew install --cask meigma/tap/ghd
```

You can also download the asset for your operating system and architecture from
[GitHub Releases](https://github.com/meigma/ghd/releases), place the `ghd`
binary somewhere on your `PATH`, and confirm it runs:

```sh
ghd --help
```

For GitHub API rate limits, authenticated requests are more reliable:

```sh
export GITHUB_TOKEN="$(gh auth token)"
```

## Download a Verified Asset

Start with a standalone download. This writes the verified release asset and its
verification evidence to the output directory, but it does not install anything.

```sh
ghd download "meigma/ghd/ghd-example@1.1.1" \
  --output "$HOME/Downloads/ghd-example"
```

After the command succeeds, the output directory contains the verified artifact
and its verification evidence:

```text
$HOME/Downloads/ghd-example/ghd-example_1.1.1_<os>_<arch>
$HOME/Downloads/ghd-example/verification.json
```

The `verification.json` file records the package, version, selected release
asset, accepted immutable release attestation, and accepted SLSA provenance
attestation.

## Index the Example Repository

Add the repository to the local package index:

```sh
ghd repo add meigma/ghd
```

List indexed packages:

```sh
ghd list
```

Show details for the example package:

```sh
ghd info ghd-example
```

## Install the Example Package

Install verifies the release first, then asks for approval before exposing
binaries:

```sh
ghd install "ghd-example@1.1.1"
```

The installed binary is linked from the managed binary directory, which defaults
to `$HOME/.local/bin` on Unix-like systems.

Confirm that the managed command works:

```sh
ghd-example version
ghd-example
```

## Check and Verify the Install

Check for available updates:

```sh
ghd check ghd-example
```

Re-verify the installed package:

```sh
ghd verify ghd-example
```

List installed packages:

```sh
ghd installed
```

## Clean Up

Remove the installed example package:

```sh
ghd uninstall ghd-example
```

Removing a repository from the index is separate from uninstalling packages:

```sh
ghd repo remove meigma/ghd
```
