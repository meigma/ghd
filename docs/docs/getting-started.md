---
title: Get Started
description: Learn the first verified download and install workflow with ghd.
---

# Get Started

This tutorial uses `meigma/ghd-test`, the fixture repository used to validate
`ghd` against real GitHub releases, release attestations, and SLSA provenance.

## Before You Start

`ghd` does not have a public release yet. When the first release is published,
download the asset for your operating system and architecture from
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
ghd download "meigma/ghd-test/ghd-test@1.1.0" \
  --output "$HOME/Downloads/ghd-functional-test/archive"
```

After the command succeeds, the output directory contains the verified artifact
and its verification evidence:

```text
$HOME/Downloads/ghd-functional-test/archive/ghd-test_1.1.0_<os>_<arch>.tar.gz
$HOME/Downloads/ghd-functional-test/archive/verification.json
```

The `verification.json` file records the package, version, selected release
asset, accepted immutable release attestation, and accepted SLSA provenance
attestation.

## Index the Fixture Repository

Add the fixture repository to the local package index:

```sh
ghd repo add meigma/ghd-test
```

List indexed packages:

```sh
ghd list
```

Show details for the fixture package:

```sh
ghd info ghd-test
```

## Install the Fixture Package

Install verifies the release first, then asks for approval before exposing
binaries:

```sh
ghd install "ghd-test@1.1.0"
```

The installed binary is linked from the managed binary directory, which defaults
to `$HOME/.local/bin` on Unix-like systems.

## Check and Verify the Install

Check for available updates:

```sh
ghd check ghd-test
```

Re-verify the installed package:

```sh
ghd verify ghd-test
```

List installed packages:

```sh
ghd installed
```

## Clean Up

Remove the installed fixture package:

```sh
ghd uninstall ghd-test
```

Removing a repository from the index is separate from uninstalling packages:

```sh
ghd repo remove meigma/ghd-test
```
