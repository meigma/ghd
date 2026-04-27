# Contributing

Thank you for your interest in contributing to `ghd`.

This guide covers questions, bug reports, feature requests, and pull requests.
For private vulnerability reporting, use [SECURITY.md](SECURITY.md) instead of
public channels.

## Asking Questions

Use [GitHub Discussions](https://github.com/meigma/ghd/discussions) for usage
questions, troubleshooting, and design discussion.

## Reporting Bugs

Report non-security bugs through
[GitHub Issues](https://github.com/meigma/ghd/issues).

Include the following details when possible:

- version, commit, or environment details
- steps to reproduce
- expected behavior
- actual behavior
- logs, screenshots, or a minimal reproduction

If you are reporting a security issue, stop and follow [SECURITY.md](SECURITY.md)
instead.

## Proposing Features

Use [GitHub Discussions](https://github.com/meigma/ghd/discussions) for feature
requests and design proposals.

For larger changes, describe the problem, the proposed approach, and any
compatibility or migration concerns before starting implementation. Keep the
proposal lightweight enough to learn from prototypes and working behavior.

## Pull Requests

Contributors should:

1. Keep changes focused and scoped to a single problem.
2. Preserve the hexagonal architecture guidance in [AGENTS.md](AGENTS.md).
3. Add or update tests when behavior changes.
4. Prefer functional testing before calling a feature complete.
5. Update documentation when user-facing behavior changes.
6. Describe the change clearly in the pull request.
7. Make sure CI passes before requesting review.

## Local Setup

Install documentation dependencies:

```sh
cd docs
npm ci
```

Useful project commands:

```sh
moon run docs:build
moon run docs:typecheck
moon ci --summary minimal
```

The `~/code/meigma/ghd-test` repository is available for complex functional
testing of `ghd`, including release-driven end-to-end tests once the CLI exists.

## Code of Conduct

This repository does not currently declare a separate Code of Conduct.

## License and Ownership

No contribution license agreement, DCO, or CLA is currently declared.
Contributions are accepted under the project's dual license: Apache-2.0 OR MIT.
