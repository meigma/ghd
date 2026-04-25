---
title: Security Model
description: How ghd verifies GitHub release assets and where its trust boundaries are.
---

# Security Model

`ghd` is intentionally narrow: it installs GitHub release assets only after the
selected bytes pass the release and provenance checks that the tool understands.
It does not offer alternate security modes, detached checksum files, arbitrary
install scripts, or hoster-controlled install destinations.

## What `ghd` Verifies

| Layer | What `ghd` checks | Why it matters |
| --- | --- | --- |
| Release record | The selected asset appears in GitHub's immutable release attestation for the requested tag. | This ties the local bytes to the release GitHub published. |
| Artifact provenance | The asset has SLSA provenance for the local artifact digest. | This ties the local bytes to a build provenance statement. |
| Signer workflow | The provenance signer workflow matches the workflow declared by `ghd.toml`. | This rejects artifacts built by an unexpected GitHub Actions workflow. |
| Source identity | The source repository and source ref must match the selected package and release. | This keeps release resolution and build identity aligned. |
| Local install boundary | Binaries are exposed only from `ghd`-managed directories. | This avoids hoster-controlled writes elsewhere on the user's machine. |

## Immutable Releases

GitHub immutable releases lock the release tag and release assets after
publication. GitHub also produces a release attestation for immutable releases.
`ghd` uses that release attestation to confirm that the local artifact digest is
part of the immutable release record.

Learn more in GitHub's
[immutable releases documentation](https://docs.github.com/code-security/supply-chain-security/understanding-your-software-supply-chain/immutable-releases).

## Artifact Attestations and SLSA Provenance

GitHub artifact attestations are signed claims about build outputs. For `ghd`,
the important provenance fact is not just that an attestation exists; it must be
for the selected artifact digest and must come from the expected GitHub Actions
workflow identity.

`ghd` requires the SLSA provenance predicate:

```text
https://slsa.dev/provenance/v1
```

Learn more from:

- [GitHub artifact attestations](https://docs.github.com/en/enterprise-cloud@latest/actions/concepts/security/artifact-attestations)
- [SLSA v1.2](https://slsa.dev/spec/v1.2/)
- [Sigstore](https://docs.sigstore.dev/)

## Signer Workflow Changes

The trusted signer workflow is declared in `ghd.toml`. During update, `ghd`
compares the signer workflow accepted for the installed package with the signer
workflow accepted for the candidate release.

If the workflow path changes, `ghd` refuses ordinary non-interactive approval:

```sh
ghd --yes --non-interactive update package
```

Approve that change only after review:

```sh
ghd --yes --approve-signer-change --non-interactive update package
```

This first signer-change flow approves workflow identity changes. It does not
pin or approve workflow digest changes as a separate trust decision yet.

## What `ghd` Does Not Prove

`ghd` does not prove that:

- the source code in the expected repository is safe;
- a maintainer did not intentionally publish malicious code;
- a GitHub account with release permissions was not compromised;
- the installed binary has no vulnerabilities;
- the build is reproducible.

Those are real supply-chain concerns, but they are outside the current `ghd`
claim. `ghd` focuses on refusing tampered release assets, missing provenance,
wrong workflow identity, and unsafe local install behavior.
