# ghd

`ghd` is an experimental CLI for installing GitHub release assets only after
the selected artifact passes strict integrity and provenance checks.

The repository currently contains the initial product/security design and docs
scaffolding. The CLI implementation is not present yet.

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

There is no installable `ghd` binary yet. Start with the design document:

- [Initial Design](docs/docs/design.md)

The proposed command shape includes repository indexing, package discovery,
verified install, update, uninstall, and doctor flows.

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
