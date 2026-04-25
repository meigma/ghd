package verification

import (
	"strings"
	"time"
)

// RunnerEnvironment identifies the GitHub Actions runner environment.
type RunnerEnvironment string

const (
	// RunnerEnvironmentGitHubHosted is the certificate value for GitHub-hosted runners.
	RunnerEnvironmentGitHubHosted RunnerEnvironment = "github-hosted"
	// GitHubReleaseSubjectAlternativeName is the certificate identity for GitHub release attestations.
	GitHubReleaseSubjectAlternativeName = "https://dotcom.releases.github.com"
	// GitHubActionsOIDCIssuer is the issuer used by GitHub Actions OIDC tokens.
	GitHubActionsOIDCIssuer = "https://token.actions.githubusercontent.com"
)

// CertificateEvidence contains non-forgeable certificate fields used by policy.
type CertificateEvidence struct {
	// Issuer is the OIDC issuer recorded by the signing certificate.
	Issuer string
	// SubjectAlternativeName is the signing certificate SAN.
	SubjectAlternativeName string
	// SourceRepository is the source repository recorded by the certificate.
	SourceRepository Repository
	// SourceRef is the source ref recorded by the certificate when available.
	SourceRef SourceRef
	// SourceDigest is the source commit digest recorded by the certificate when available.
	SourceDigest Digest
	// SignerWorkflow is the workflow identity recorded by the certificate.
	SignerWorkflow WorkflowIdentity
	// SignerDigest is the signer workflow digest recorded by the certificate when available.
	SignerDigest Digest
	// RunnerEnvironment is the runner environment recorded by the certificate.
	RunnerEnvironment RunnerEnvironment
}

func (c CertificateEvidence) hasGitHubReleaseIdentity() bool {
	return strings.EqualFold(c.SubjectAlternativeName, GitHubReleaseSubjectAlternativeName)
}

func (c CertificateEvidence) hasGitHubActionsIssuer() bool {
	return strings.EqualFold(c.Issuer, GitHubActionsOIDCIssuer)
}

// VerifiedTimestamp describes one trusted timestamp observation.
type VerifiedTimestamp struct {
	// Kind identifies the timestamp source.
	Kind string
	// Time is the verified timestamp time.
	Time time.Time
}

func (t VerifiedTimestamp) valid() bool {
	return t.Kind != "" && !t.Time.IsZero()
}

// Attestation is an opaque attestation bundle fetched by an adapter.
type Attestation struct {
	// ID identifies the attestation for evidence and diagnostics.
	ID string
	// Bundle is adapter-owned bundle data passed through to BundleVerifier.
	Bundle any
}

// VerifiedAttestation contains trusted data returned by BundleVerifier.
type VerifiedAttestation struct {
	// Attestation is the source attestation that was verified.
	Attestation Attestation
	// Statement contains trusted in-toto statement fields.
	Statement Statement
	// Certificate contains trusted certificate evidence.
	Certificate CertificateEvidence
	// VerifiedTimestamps contains trusted timestamp evidence.
	VerifiedTimestamps []VerifiedTimestamp
}

func (a VerifiedAttestation) hasTimestampEvidence() bool {
	for _, timestamp := range a.VerifiedTimestamps {
		if timestamp.valid() {
			return true
		}
	}
	return false
}
