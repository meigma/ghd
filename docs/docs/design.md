---
title: Initial Design
description: Initial product and security design for GitHub Downloader.
---

# GitHub Downloader Initial Design

Status: initial design. This document describes the first coherent shape of the
project, not a frozen architecture.

Prototype status: `list`, `info`, `download`, `install`, repository indexing,
installed-state, `check`, `verify`, `doctor`, `update`, `uninstall`, binary
ownership collision preflight, and JSON output for result-oriented commands now
exist as working slices.

GitHub Downloader (`ghd`) is a CLI for securely installing binaries from GitHub
releases. It is intentionally opinionated: a compatible repository must publish
GitHub release assets, use GitHub-native artifact attestations, and publish
immutable releases. `ghd` should provide one verification path, not a menu of
security modes.

## Development Direction

Security is paramount. `ghd` is a security tool, and users must be able to trust
both its behavior and its failure modes. When security conflicts with
convenience, compatibility, or implementation speed, security wins.

Simplicity is a security property. Every line of code is another surface to
understand, test, review, and maintain. Prefer small, proven, boring solutions
over clever abstractions. Add code only when it earns its place in the verified
install path or the user experience around that path.

The type system is part of the security boundary. Use strong types to encode
validated repository names, package names, versions, digests, paths, release
tags, verification results, and policy decisions. Avoid passing raw strings or
loosely shaped maps across package boundaries when a narrow type can prevent
misuse.

User experience matters. `ghd` is intended to make secure installation normal
for real users, so confusing workflows or unpleasant output directly weaken the
goal. The CLI should be understandable, scriptable, and pleasant to use, with
clear errors and polished terminal output where that helps comprehension.

Code hygiene matters. This is an open source project that should welcome human
contributors. Code should be readable, discoverable, and right-sized: sensible
package boundaries, clear function names, focused files, strong doc comments on
exported APIs, and tests that explain behavior rather than implementation
details.

## Goals

- Install GitHub release binaries only after verification succeeds.
- Make the secure path the normal path for users and hosters.
- Support repositories that publish one installable package or many installable
  packages.
- Keep installation behavior predictable by writing only to `ghd`-managed user
  directories.
- Keep hoster configuration small enough to validate with real repositories.

## Non-Goals

- No central package registry.
- No attached checksum files.
- No attached attestation files.
- No alternate attestation sources for GitHub release assets.
- No arbitrary install scripts.
- No hoster-selected absolute install destinations.
- No dependency management.
- No background auto-update system.
- No global package search in the first release.

## Core Model

The repository is the trust and discovery boundary. A package is the installable
unit inside that repository.

This lets `ghd` support simple repositories and monorepos without changing the
core model later:

- one repository, one package, one release stream;
- one repository, many packages, one release stream;
- one repository, many packages, independent tag patterns.

If a user installs by package name alone, the local index must resolve that name
unambiguously. If more than one indexed repository exposes the same package or
binary name, the user must qualify the install target with `owner/repo/package`.

## Hoster Requirements

A compatible repository must:

- include a root `ghd.toml` configuration file;
- publish installable artifacts as GitHub release assets;
- enable GitHub release immutability for releases intended for `ghd`;
- generate GitHub artifact attestations for each installable release asset;
- use the SLSA provenance predicate for artifact attestations;
- declare the trusted signer workflow in `ghd.toml`.

A compatible repository must not require `ghd` to:

- read a checksum file;
- read an attestation file attached to the release;
- run a post-install script;
- write outside the managed install root.

## Repository Configuration

`ghd.toml` lives at the repository root.

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
path = "foo"

[[packages]]
name = "bar"
description = "Bar CLI"
tag_pattern = "bar-v${version}"

[[packages.assets]]
os = "darwin"
arch = "arm64"
pattern = "bar_${version}_darwin_arm64.tar.gz"

[[packages.binaries]]
path = "bin/bar"
```

Configuration rules:

- `version` is the `ghd.toml` schema version.
- `[provenance]` is repository-wide in the first design.
- `signer_workflow` identifies the workflow whose OIDC identity must appear in
  the artifact attestation certificate.
- If the hoster uses a reusable trusted builder, `signer_workflow` is the
  reusable workflow, not the caller workflow.
- `[[packages]]` is always an array, even for one-package repositories.
- `packages.name` is the installable package name within the repository.
- `tag_pattern` is optional if the package uses the repository's normal release
  tags, such as `v${version}`.
- `[[packages.assets]]` maps OS and architecture to a GitHub release asset name
  pattern.
- `[[packages.binaries]] path` is the relative path to an executable inside the
  verified asset or extracted archive.
- The exposed command name is `basename(path)`.
- Binary paths must be relative paths without `..`.

The config does not include the GitHub owner/repository because `ghd` fetches
the config from the repository the user selected. It also does not include an
`immutable_release` setting because immutable release validation is mandatory.

## User Commands

Repository indexing:

```sh
ghd repo add owner/repo
ghd repo remove owner/repo
ghd repo refresh [owner/repo | --all]
ghd repo list
```

Package discovery:

```sh
ghd list
ghd list owner/repo
ghd info <name | owner/repo[/package]>
```

Installation:

```sh
ghd install foo
ghd install owner/repo/foo
ghd install owner/repo/foo@1.2.3
```

Installed package management:

```sh
ghd installed
ghd check [name | --all]
ghd update <name | --all>
ghd verify [name | owner/repo/package | --all]
ghd uninstall <name>
ghd doctor
```

Behavior notes:

- `repo add` fetches `ghd.toml` and records the repository in the local index.
- `install` re-indexes added repositories before resolving an unqualified
  package name.
- `list` without a repository reads the local index.
- `list owner/repo` fetches and displays that repository's packages without
  adding it.
- `info owner/repo` selects the only declared package when the repository has
  exactly one package; otherwise the user must qualify `owner/repo/package`.
- `install owner/repo/foo` can be a one-off install without adding the
  repository to the index.
- `install` refuses binary-name collisions against active installed packages
  before downloading release assets.
- Interactive `install` shows transient status with byte-level download progress
  when the asset size is known, presents verified release and provenance facts
  after verification behind `View details`, and asks before exposing binaries.
  `--yes --non-interactive` is the automation path: it disables prompts, color,
  and transient UI while explicitly approving the verified install. The stable
  stdout `binary PATH` contract is reserved for non-interactive installs.
- `check` is read-only and reports available updates for installed packages.
- `update` applies an available update through the same verification path as
  install and refuses updates that would expose a binary owned by another
  installed package. Interactive update uses transient status, byte-level
  download progress, and verified-artifact approval before swapping binaries;
  `--yes --non-interactive` keeps result output plain for automation.
- `doctor` checks PATH setup, local directory permissions, GitHub connectivity,
  and authentication/rate-limit readiness.
- `list`, `info`, `installed`, `check`, `verify`, `update`, `doctor`, and
  `repo list` support `--json` for scriptable result output.

## Local State

`ghd` should own a small set of user-scoped directories. The exact paths can be
platform-specific, but the first Unix-like shape is:

```text
~/.local/share/ghd/index/
~/.local/share/ghd/store/
~/.local/state/ghd/
~/.local/bin/
```

The store should be content-addressed or digest-keyed enough to make audit and
rollback simple:

```text
~/.local/share/ghd/store/github.com/owner/repo/package/version/asset-digest/
  artifact
  extracted/
  verification.json
```

`ghd` exposes binaries by linking from the managed bin directory to the verified
store path. The hoster controls which binary paths are exposed, but never where
they are installed on the user's machine.

If two installed packages would expose the same binary name, `ghd` refuses the
second install or update instead of silently overwriting the command. Richer
ownership transfer or shim behavior can be introduced later.

## Install Pipeline

For `ghd install owner/repo/foo@1.2.3`:

1. Fetch or refresh `ghd.toml` from `owner/repo`.
2. Resolve package `foo`.
3. Resolve version `1.2.3` to the expected release tag using `tag_pattern`.
4. Resolve the GitHub release and matching asset for the current OS and
   architecture.
5. Download the asset into a temporary, non-executable location.
6. Verify the immutable GitHub release attestation for the tag and asset.
7. Verify the GitHub artifact provenance attestation for the local asset bytes.
8. Present verified facts and require approval unless `--yes` was supplied.
9. Extract the archive if needed.
10. Copy or link only the configured binary paths into the store.
11. Expose binary links from the managed bin directory.
12. Record installed package metadata and verification evidence.

The temporary download should not be executable. Installation should only expose
the final verified binary after all verification steps succeed.

## Release Verification

Immutable release validation protects the GitHub release layer. `ghd` should
verify that the downloaded local file appears in GitHub's signed release
attestation for the concrete tag.

Native implementation shape:

1. Resolve the release tag to its tag ref object digest.
2. Fetch GitHub release attestations for that tag ref object digest.
3. Require GitHub's release predicate.
4. Require the attestation tag to match the requested tag.
5. Require the local asset digest to appear in the release attestation subjects.
6. Verify the release attestation bundle against GitHub's Sigstore trust root and
   release certificate identity.

This proves that the local bytes correspond to an asset in the immutable GitHub
release record. It does not prove that the asset was built by a trusted workflow.

## Provenance Verification

Artifact provenance validation protects the build layer. `ghd` should verify
that the local asset bytes have SLSA provenance from the expected GitHub Actions
workflow identity.

Native implementation shape:

1. Compute the local asset digest.
2. Fetch GitHub artifact attestations for that repository and subject digest.
3. Require predicate type `https://slsa.dev/provenance/v1`.
4. Verify the Sigstore bundle.
5. Require the expected source repository.
6. Require the expected signer workflow.
7. Require the expected source ref or source digest when available.
8. Require GitHub-hosted runners.

The attestation subject digest is the checksum authority. `ghd` should not parse
or trust release checksum files for install security.

Only certificate and timestamp verification material should be treated as
non-forgeable by the workflow. SLSA predicate contents are useful, but policy
must not rely only on workflow-controlled predicate fields when the same claim is
available in the certificate identity.

## Implementation Notes

`ghd` should implement verification natively. The GitHub CLI is useful as a
reference implementation and behavioral oracle, but `ghd` should not shell out
to `gh`.

Likely Go dependencies:

- `github.com/sigstore/sigstore-go`
- `github.com/sigstore/protobuf-specs`
- `github.com/in-toto/attestation/go/v1`
- `github.com/klauspost/compress/snappy`

`ghd` should not import GitHub CLI internal packages directly. They are not a
stable library boundary.

## Go Module Shape

The first implementation should be a root Go module for the CLI product:

```text
module github.com/meigma/ghd
```

The repository should not start with a public `pkg/` API. `ghd` is a command
first, and the stable API surface should emerge from the verified install flow
after the CLI has proved which abstractions are useful.

Initial package layout:

```text
cmd/ghd/main.go

internal/
  cli/          # Cobra commands, flags, output, and terminal concerns.
  config/       # Viper-backed runtime config, paths, auth, and environment.
  runtime/      # Dependency wiring between use cases and adapters.
  app/          # Use cases and the ports each use case consumes.
  manifest/     # ghd.toml schema, validation, tag patterns, and asset matching.
  catalog/      # Repository index, package resolution, and ambiguity handling.
  verification/ # Release and provenance policy plus verification evidence.
  state/        # Installed package metadata and managed store records.

  adapters/
    github/     # GitHub releases, repository content, and attestation lookup.
    sigstore/   # Sigstore bundle and certificate verification.
    filesystem/ # Local index, store, temporary downloads, and binary links.
    archive/    # Archive extraction and path traversal defense.
    toml/       # ghd.toml decoding and encoding.

  version/
```

`cmd/ghd/main.go` should install a signal-aware context and execute the root
command. The `internal/cli` package is an adapter: it owns Cobra command
construction, flag definitions, user-facing output, and handoff into the
application layer. It should not contain install, verification, indexing, or
filesystem business logic.

`internal/runtime` should wire the concrete adapters to the application use
cases. Use Viper as an instance dependency loaded through `internal/config`;
avoid package-global Viper state and avoid passing application services through
`context.Context`.

Core behavior should follow hexagonal boundaries:

- use cases live in `internal/app`;
- interfaces live near the use case that consumes them;
- adapters implement those interfaces at the edge;
- domain packages must not import Cobra, Viper, GitHub clients, terminal UI
  packages, or concrete filesystem adapters;
- adapters may depend on stable core types, but core packages must not depend on
  adapter packages.

For the first prototype, create only the packages needed to prove one vertical
install path. That likely means `cmd/ghd`, `internal/cli/install`,
`internal/config`, `internal/runtime`, `internal/app`, `internal/manifest`,
`internal/verification`, and the concrete adapters needed for one real GitHub
release. Add `catalog`, repository management commands, update flows, and richer
state only after the first verified install works.

Unit tests should live beside the packages they cover and focus on observable
behavior. CLI behavior should be tested with filesystem-based test scripts once
the first command flow exists. End-to-end functional testing should use
`~/code/meigma/ghd-test` for real release/install exercises.

## Security Boundaries

In scope:

- network tampering with downloaded release assets;
- release asset replacement after publication;
- missing artifact provenance;
- provenance from the wrong workflow;
- install ambiguity from similarly named packages or assets;
- weaker hoster build paths than declared;
- archive path traversal attempts.

Not solved initially:

- malicious source code in the expected repository;
- a compromised maintainer intentionally changing the trusted workflow;
- a compromised GitHub account with enough permission to publish a valid
  release;
- vulnerabilities in the installed binary;
- reproducible-build comparison;
- sandboxed execution of installed binaries.

## First Prototype

The first vertical slice should prove the complete path for one real repository:

1. Read a minimal local or repository `ghd.toml`.
2. Resolve one package and platform asset.
3. Download the asset.
4. Verify immutable release attestation.
5. Verify SLSA artifact provenance with trusted signer workflow policy.
6. Extract one configured binary path.
7. Link it into the managed bin directory.
8. Record `verification.json`.

After the verified install, indexing, installed-state, uninstall, read-only
check, package-discovery, collision preflight, broader lifecycle slices,
structured output for result-oriented commands, and the first interactive
install/update UX passes, the next slices should focus on remaining polish and
release-readiness gaps. Byte-level download progress for the standalone
`download` command is still deferred.

## Open Questions

- Should `tag_pattern` be required for all packages, or can single-package
  repositories rely on a default?
- What local database format is enough for the first release?
- Should binary exposure use symlinks only, or should `ghd` use shims from the
  start?
- How should users approve an upstream signer workflow change after a repository
  has already been added?
- What is the exact UX for future binary ownership transfer or shim-based
  coexistence between installed packages?

## References

- [SLSA v1.2 requirements](https://slsa.dev/spec/v1.2/requirements)
- [SLSA build requirements](https://slsa.dev/spec/v1.2/build-requirements)
- [GitHub artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations)
- [GitHub SLSA Build Level 3 guidance](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/increase-security-rating)
- [GitHub immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)
- [`gh attestation verify`](https://cli.github.com/manual/gh_attestation_verify)
- [`gh release verify-asset`](https://cli.github.com/manual/gh_release_verify-asset)
