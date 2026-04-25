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
installed-state management, read-only update checks, batch re-verification,
environment diagnostics, batch updates, and JSON output for result-oriented
discovery and lifecycle commands:

```sh
go run ./cmd/ghd download owner/repo/package@version --output ./out
go run ./cmd/ghd repo add owner/repo --index-dir ./index
go run ./cmd/ghd repo list --index-dir ./index
go run ./cmd/ghd list --index-dir ./index
go run ./cmd/ghd list owner/repo
go run ./cmd/ghd info package --index-dir ./index
go run ./cmd/ghd info owner/repo/package
go run ./cmd/ghd install owner/repo/package --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd install owner/repo/package@version --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd install package --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd install package@version --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd --yes --non-interactive install owner/repo/package@version --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd installed --state-dir ./state
go run ./cmd/ghd check --state-dir ./state --all
go run ./cmd/ghd verify package --state-dir ./state
go run ./cmd/ghd verify --state-dir ./state --all
go run ./cmd/ghd doctor --index-dir ./index --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd update package --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd --yes --non-interactive update package --state-dir ./state --store-dir ./store --bin-dir ./bin
go run ./cmd/ghd update --state-dir ./state --store-dir ./store --bin-dir ./bin --all
go run ./cmd/ghd uninstall package --state-dir ./state --store-dir ./store --bin-dir ./bin
```

Commands that return stable row-style results support `--json`: `list`, `info`,
`installed`, `check`, `verify`, `update`, `doctor`, and `repo list`.

Download, install, check, and update now fail closed unless the selected release
tag contains a root `ghd.toml`. The default-branch manifest is only used to
discover a candidate release tag; signer workflow, asset pattern, and binary
path policy come from the manifest at that tag. When `install` omits
`@version`, it resolves the latest eligible stable release for the package on
the target platform before verification.

Interactive `download` now uses stderr-first terminal UX: transient status,
byte-level download progress when GitHub reports an asset size, and a final
verified summary on stderr. Machine-readable `artifact PATH` and
`verification PATH` lines stay on stdout only in the plain automation path,
including non-TTY and `--non-interactive` usage.

Interactive `install` and `update` run with transient terminal status, show
byte-level download progress when GitHub reports an asset size, and ask for
approval after verification but before exposing or swapping binaries. The
approval prompt summarizes the source, destination, and trust result, with full
provenance facts behind `View details`. Automation should pass
`--yes --non-interactive` to keep output plain and approve the verified action
without prompting; `install` emits the stable `binary PATH` stdout lines only in
non-interactive mode, while `update` preserves its result rows and `--json`
output.

Interactive `uninstall` now uses transient terminal status plus a final stderr
summary of what was removed, but it remains immediate and non-confirming by
design. `--non-interactive` keeps the existing one-line stderr result without
any richer terminal framing.

`list`, `info`, `check`, `verify`, `doctor`, and `repo list` now render richer
human-oriented views when stdout is a terminal. Their automation-facing
contracts stay unchanged: `--json` preserves structured output, and
`--non-interactive` forces the existing plain row or labeled text output
without transient status text.

Interactive `repo add`, `repo refresh`, and `repo remove` now show transient
stderr status plus concise stderr summaries on a terminal, while keeping their
existing one-line stderr output in the plain non-interactive path. `repo
remove` still updates only the local index; it does not uninstall anything.

Start with the design document for the intended full product shape:

- [Initial Design](docs/docs/design.md)

`check`, `verify`, and `doctor` are intentionally read-only in the current
prototype. Binary ownership collisions are refused early; richer ownership
transfer, shim UX, and structured output for mutating status-only commands
beyond the current human-facing terminal summaries remain future work.

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
