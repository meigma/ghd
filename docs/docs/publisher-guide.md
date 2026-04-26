---
title: Publisher Guide
description: Publish GitHub releases that ghd can verify and install.
---

# Publisher Guide

This guide is for maintainers who want their GitHub release assets to be
installable through `ghd`.

`ghd` is intentionally narrow. It expects a repository to publish explicit
GitHub release assets, use GitHub's immutable release model, and generate
GitHub Actions provenance for the shipped bytes.

## Compatibility Contract

Before `ghd` can trust a repository, the repository needs all of the following:

- a root `ghd.toml` file on the default branch so `ghd` can discover packages;
- the same `ghd.toml` file present on the published release tag, because
  release-tag metadata is the trust-sensitive source for install, download,
  check, and update decisions;
- explicit GitHub release assets for each supported platform;
- immutable releases enabled in GitHub so published tags and assets cannot be
  changed after publication;
- GitHub Actions artifact attestations for the shipped assets, with a stable
  signer workflow path declared in `ghd.toml`.

`ghd` does not install GitHub's automatically generated source archives. Publish
real release assets instead.

## Configure `ghd.toml`

The repository manifest declares what package names exist, which tags contain a
package release, which asset name matches each platform, and which GitHub
Actions workflow is trusted to sign provenance.

```toml
version = 1

[provenance]
signer_workflow = "owner/repo/.github/workflows/release.yml"

[[packages]]
name = "tool"
description = "Tool CLI"
tag_pattern = "tool-v${version}"

[[packages.assets]]
os = "darwin"
arch = "arm64"
pattern = "tool_${version}_darwin_arm64"

[[packages.assets]]
os = "linux"
arch = "amd64"
pattern = "tool_${version}_linux_amd64"

[[packages.binaries]]
path = "tool"
```

Important constraints:

- `provenance.signer_workflow` is repository-wide in `ghd.toml`. Per-package
  signer workflows are not supported.
- `tag_pattern` and each asset `pattern` must contain exactly one
  `${version}` token.
- `packages.binaries.path` is the relative path to the exposed binary. For
  direct-binary assets, this is usually just the filename. For archive assets,
  it is the relative path inside the extracted archive.

See [Reference](reference.md#ghdtoml) for the full schema and validation rules.

## Enable Immutable Releases

GitHub recommends a draft-first publish flow for immutable releases:

1. Create the release as a draft.
2. Attach all release assets to the draft.
3. Publish the draft release.

Enable release immutability in GitHub before relying on `ghd` compatibility.
For a repository, GitHub documents this under `Settings` -> `Releases` ->
`Enable release immutability`.

If your repository uses tag protection or tag rulesets, the automation identity
that creates release tags must be allowed to create them. Otherwise the release
pipeline may produce a draft release without a usable tag.

## Publish Provenance from GitHub Actions

`ghd` expects the release assets to have GitHub Actions provenance. The exact
build tool is up to you, but the release workflow needs to:

1. build the final release assets;
2. generate a checksums file for the shipped assets;
3. optionally generate SBOM files;
4. upload all assets to the draft release;
5. attest the shipped subjects with `actions/attest`;
6. publish the draft release after assets and attestations are in place.

This example shows the minimum GitHub Actions shape. It assumes the pushed tag
already exists and that the workflow either creates or reuses a draft release
for that tag.

```yaml
name: Release

on:
  push:
    tags:
      - "tool-v*"

permissions: {}

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      id-token: write
      attestations: write
    steps:
      - name: Checkout
        uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6.0.2
        with:
          fetch-depth: 0
          persist-credentials: false

      - name: Create draft release if needed
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          draft="$(gh release view "${GITHUB_REF_NAME}" \
            --repo "${GITHUB_REPOSITORY}" \
            --json isDraft \
            --jq .isDraft \
            2>/dev/null || true)"

          if [[ -z "${draft}" ]]; then
            gh release create "${GITHUB_REF_NAME}" \
              --repo "${GITHUB_REPOSITORY}" \
              --draft \
              --verify-tag \
              --title "${GITHUB_REF_NAME}" \
              --notes ""
          elif [[ "${draft}" != "true" ]]; then
            echo "release ${GITHUB_REF_NAME} already exists and is not a draft" >&2
            exit 1
          fi

      - name: Build release assets
        run: ./scripts/build-release.sh

      - name: Upload assets to the draft release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          gh release upload "${GITHUB_REF_NAME}" \
            dist/tool_* \
            dist/checksums.txt \
            --repo "${GITHUB_REPOSITORY}" \
            --clobber

      - name: Generate provenance attestation
        uses: actions/attest@59d89421af93a897026c735860bf21b6eb4f7b26 # v4.1.0
        with:
          subject-checksums: ./dist/checksums.txt

      - name: Publish immutable release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          gh release edit "${GITHUB_REF_NAME}" \
            --repo "${GITHUB_REPOSITORY}" \
            --draft=false
```

Notes:

- The attestation step needs `id-token: write` and `attestations: write`.
- `subject-checksums` is the simplest way to attest many release files at once.
- If you also want linked-artifact storage records, GitHub documents an
  additional `artifact-metadata: write` permission for that path.
- SBOM assets are optional for `ghd`, but shipping them alongside binaries is a
  reasonable default. If you publish them, add those files to the
  `gh release upload` step too.

## Verify the Published Release

After publishing, validate the release the same way a consumer would.

Verify that the GitHub release exists and is immutable:

```sh
gh release verify tool-v1.2.3 -R owner/repo
```

Verify that a downloaded local asset exactly matches the published release
asset:

```sh
gh release verify-asset tool-v1.2.3 ./tool_1.2.3_darwin_arm64 -R owner/repo
```

This checks uploaded release assets only. GitHub's generated source zip and
tarball archives are not valid `ghd` install targets.

Verify GitHub Actions provenance for one downloaded asset:

```sh
gh attestation verify ./tool_1.2.3_darwin_arm64 \
  --repo owner/repo \
  --signer-workflow owner/repo/.github/workflows/release.yml \
  --source-ref refs/tags/tool-v1.2.3 \
  --deny-self-hosted-runners
```

If those checks pass and the repository manifest matches the shipped assets,
`ghd` has the metadata it needs to discover, verify, and install the package.
