# ghd

`ghd` is a secure installer for programs distributed through GitHub Releases.
Before installing anything, it confirms that the binary you are about to run is
the exact artifact a project's maintainers published, built by the workflow
they declared, and recorded in GitHub's immutable release log.

That protects you against tampered downloads, swapped assets, and releases
built by an unexpected pipeline. To make those guarantees, `ghd` uses GitHub's
immutable releases, artifact attestations, and SLSA provenance, all checked
locally before any binary is exposed on your system.

## Installation

On macOS, install `ghd` from the Homebrew tap:

```sh
brew install --cask meigma/tap/ghd
```

You can also download the binary for your operating system and architecture
from [GitHub Releases](https://github.com/meigma/ghd/releases) and place `ghd`
on your `PATH`.

Confirm the binary runs:

```sh
ghd --help
```

For higher GitHub API rate limits, export an authenticated token:

```sh
export GITHUB_TOKEN="$(gh auth token)"
```

## Quick Start

Verify and download one release asset without installing it:

```sh
ghd download owner/repo/package@version --output ./out
```

Index a repository and install one of its packages:

```sh
ghd repo add owner/repo
ghd install package
```

Check, update, and re-verify installed packages:

```sh
ghd check package
ghd update package
ghd verify package
```

The [getting started guide](docs/docs/getting-started.md) walks through the same
flow against a live release end to end.

## Commands

| Command | Purpose |
| --- | --- |
| `ghd download` | Verify and download one release asset. |
| `ghd repo add`, `list`, `refresh`, `remove` | Manage indexed repositories. |
| `ghd list`, `ghd info` | Discover packages from the index or directly from a repository. |
| `ghd install`, `ghd uninstall` | Install or remove a package. |
| `ghd installed` | List installed packages. |
| `ghd check`, `ghd update`, `ghd verify` | Detect updates, apply them, or re-verify installed packages. |
| `ghd doctor` | Check local environment readiness. |

Commands with stable result data accept `--json`: `list`, `info`, `installed`,
`check`, `verify`, `update`, `doctor`, and `repo list`.

Use `--non-interactive` for plain output suitable for scripts. Use `--yes` to
approve verified install actions and ordinary verified updates without prompts.

## Configuration

A compatible repository declares its packages in a root `ghd.toml`:

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
pattern = "foo_${version}_darwin_arm64"

[[packages.binaries]]
path = "foo"
```

Archive assets are also supported. See the [reference](docs/docs/reference.md)
for the full schema and the [publisher guide](docs/docs/publisher-guide.md) for
how to ship a `ghd`-compatible release.

## Verification

For every download, install, update, or `verify` run, `ghd` checks that:

1. The selected asset is part of an immutable GitHub release attestation for the
   requested tag.
2. The local artifact digest has SLSA provenance.
3. The provenance signer workflow matches the workflow declared in `ghd.toml`.
4. The source repository and source ref match the selected package and release.

Installed binaries are exposed only from `ghd`-managed directories. The
[security model](docs/docs/security-model.md) explains what `ghd` does and does
not claim to prove.

## Documentation

Full documentation is published at <https://ghd.meigma.dev>.

Local source:

- [Get started](docs/docs/getting-started.md)
- [Manage packages](docs/docs/manage-packages.md)
- [Security model](docs/docs/security-model.md)
- [Publisher guide](docs/docs/publisher-guide.md)
- [Reference](docs/docs/reference.md)
- [Design history](docs/docs/design.md)

## Support

Use [GitHub Discussions](https://github.com/meigma/ghd/discussions) for usage
questions and design discussion.

Use [GitHub Issues](https://github.com/meigma/ghd/issues) for non-security bug
reports.

For private vulnerability reporting, see [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, testing expectations,
and pull request workflow.

## License

`ghd` is dual-licensed under either of:

- [Apache License, Version 2.0](LICENSE-APACHE)
- [MIT License](LICENSE-MIT)

at your option. See [LICENSE](LICENSE) for the dual-license notice.
