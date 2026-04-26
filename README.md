# ghd

`ghd` is an experimental CLI for installing GitHub release assets only after the
selected artifact passes strict release integrity and provenance checks.

It is built for repositories that publish binaries through GitHub Releases,
enable immutable releases, and generate GitHub artifact attestations with SLSA
provenance.

## Status

`ghd` does not have a public release yet. When the first release is available,
install it by manually downloading the matching asset from
[GitHub Releases](https://github.com/meigma/ghd/releases) and placing the `ghd`
binary on your `PATH`. A dogfood self-update path is planned later.

## Usage

Verify and download one release asset without installing it:

```sh
ghd --non-interactive download owner/repo/package@version --output ./out
```

Index a repository and install one package:

```sh
ghd --non-interactive repo add owner/repo
ghd --yes --non-interactive install package
```

Check, update, and re-verify installed packages:

```sh
ghd check package
ghd --yes --non-interactive update package
ghd verify package
```

Commands with stable result data support `--json`: `list`, `info`, `installed`,
`check`, `verify`, `update`, `doctor`, and `repo list`.

## Documentation

The user-facing docs site is planned for <https://ghd.meigma.dev>.

Local source:

- [Get started](docs/docs/getting-started.md)
- [Manage packages](docs/docs/manage-packages.md)
- [Security model](docs/docs/security-model.md)
- [Reference](docs/docs/reference.md)
- [Design history](docs/docs/design.md)

## Development

Prerequisites:

- Go
- Node.js 20 or newer for the documentation site
- npm
- Moon, when running the same task graph used by CI

Install documentation dependencies:

```sh
cd docs
npm ci
```

Build and typecheck the docs:

```sh
moon run docs:build
moon run docs:typecheck
```

Run the Go tests:

```sh
go test ./...
```

Cloudflare Pages should use:

- project name `ghd`
- custom domain `ghd.meigma.dev`
- production branch `master`
- `docs` as the root directory
- `npm run build` as the build command
- `build` as the build output directory

The checked-in [docs/wrangler.jsonc](docs/wrangler.jsonc) records the
Pages-side build output directory for Wrangler-based local development or
deployments.

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
