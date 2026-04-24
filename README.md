# ghd

`ghd` is an experimental CLI for discovering, verifying, and installing GitHub
release assets only after the selected artifact passes strict integrity and
provenance checks.

The repository currently contains the initial product/security design, docs
scaffolding, and early `download` / `install` prototype commands.

## Quick Start

### Prerequisites

- Node.js 20 or newer for the documentation site
- npm
- Moon, when running the same task graph used by CI

### Preview the docs

```sh
cd docs
npm ci
npm run start
```

### Build the docs

```sh
moon run docs:build
```

## Usage

The prototype command surface currently supports package discovery, direct
verified downloads, repository indexing, one-off verified installs,
installed-state management, read-only update checks, re-verification,
environment diagnostics, and single-target updates:

```sh
go run ./cmd/ghd download owner/repo/package@version --output ./out
go run ./cmd/ghd repo add owner/repo --index-dir ./index
go run ./cmd/ghd repo list --index-dir ./index
go run ./cmd/ghd list --index-dir ./index
go run ./cmd/ghd list owner/repo
go run ./cmd/ghd info package --index-dir ./index
go run ./cmd/ghd info owner/repo/package
go run ./cmd/ghd install owner/repo/package@version --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd install package@version --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd installed --state-dir ./state
go run ./cmd/ghd check --state-dir ./state --all
go run ./cmd/ghd verify package --state-dir ./state
go run ./cmd/ghd doctor --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd update package --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd uninstall package --state-dir ./state --store-dir ./store --bin-dir ./bin
```

Start with the design document for the intended full product shape:

- [Initial Design](docs/docs/design.md)

`check`, `verify`, and `doctor` are intentionally read-only in the current
prototype. Bulk lifecycle flows and richer collision ownership UX remain future
work.

## Documentation

- Docs landing page: [docs/docs/index.md](docs/docs/index.md)
- Initial architecture and security design: [docs/docs/design.md](docs/docs/design.md)
- Project process and agent guidance: [AGENTS.md](AGENTS.md)

## Support

Use [GitHub Discussions](https://github.com/meigma/ghd/discussions) for usage
questions and design discussion.

Use [GitHub Issues](https://github.com/meigma/ghd/issues) for non-security bug
reports and implementation tasks.

Do not report vulnerabilities in public channels. See [SECURITY.md](SECURITY.md).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, testing expectations,
and pull request workflow.

## Security

See [SECURITY.md](SECURITY.md) for supported versions and private vulnerability
reporting.

## License

No license has been declared yet. Unless a license file is added, all rights are
reserved by the repository owner.
