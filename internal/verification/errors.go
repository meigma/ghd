package verification

import (
	"errors"
	"fmt"
)

// ErrorKind identifies a verification failure category.
type ErrorKind string

const (
	// KindInvalidRequest means the verification request is incomplete or invalid.
	KindInvalidRequest ErrorKind = "invalid_request"
	// KindDigest means the local artifact digest could not be computed or used.
	KindDigest ErrorKind = "digest"
	// KindResolveRelease means the release tag could not be resolved.
	KindResolveRelease ErrorKind = "resolve_release"
	// KindFetchReleaseAttestations means release attestations could not be loaded.
	KindFetchReleaseAttestations ErrorKind = "fetch_release_attestations"
	// KindNoReleaseAttestation means no release attestation was available.
	KindNoReleaseAttestation ErrorKind = "no_release_attestation"
	// KindReleasePredicateMismatch means a release attestation used the wrong predicate type.
	KindReleasePredicateMismatch ErrorKind = "release_predicate_mismatch"
	// KindReleaseTagMismatch means no verified release attestation matched the requested tag.
	KindReleaseTagMismatch ErrorKind = "release_tag_mismatch"
	// KindReleaseSubjectMismatch means no verified release attestation contained the asset digest.
	KindReleaseSubjectMismatch ErrorKind = "release_subject_mismatch"
	// KindReleaseSignerMismatch means a release attestation was not issued by GitHub's release identity.
	KindReleaseSignerMismatch ErrorKind = "release_signer_mismatch"
	// KindFetchProvenanceAttestations means provenance attestations could not be loaded.
	KindFetchProvenanceAttestations ErrorKind = "fetch_provenance_attestations"
	// KindNoProvenanceAttestation means no provenance attestation was available.
	KindNoProvenanceAttestation ErrorKind = "no_provenance_attestation"
	// KindProvenancePredicateMismatch means a provenance attestation used the wrong predicate type.
	KindProvenancePredicateMismatch ErrorKind = "provenance_predicate_mismatch"
	// KindProvenanceSubjectMismatch means no verified provenance attestation contained the asset digest.
	KindProvenanceSubjectMismatch ErrorKind = "provenance_subject_mismatch"
	// KindProvenanceIssuerMismatch means provenance did not come from GitHub Actions OIDC.
	KindProvenanceIssuerMismatch ErrorKind = "provenance_issuer_mismatch"
	// KindProvenanceIdentityMismatch means provenance did not use the expected certificate identity.
	KindProvenanceIdentityMismatch ErrorKind = "provenance_identity_mismatch"
	// KindSourceRepositoryMismatch means provenance came from the wrong source repository.
	KindSourceRepositoryMismatch ErrorKind = "source_repository_mismatch"
	// KindSignerWorkflowMismatch means provenance was signed by the wrong workflow.
	KindSignerWorkflowMismatch ErrorKind = "signer_workflow_mismatch"
	// KindSourceRefMismatch means provenance used an unexpected source ref.
	KindSourceRefMismatch ErrorKind = "source_ref_mismatch"
	// KindSourceDigestMismatch means provenance used an unexpected source digest.
	KindSourceDigestMismatch ErrorKind = "source_digest_mismatch"
	// KindRunnerEnvironmentMismatch means provenance was not produced on GitHub-hosted runners.
	KindRunnerEnvironmentMismatch ErrorKind = "runner_environment_mismatch"
	// KindTimestampMissing means a verified bundle did not include trusted timestamp evidence.
	KindTimestampMissing ErrorKind = "timestamp_missing"
	// KindSignerDigestMismatch means provenance used an unexpected signer workflow digest.
	KindSignerDigestMismatch ErrorKind = "signer_digest_mismatch"
	// KindBundleVerification means cryptographic bundle verification failed.
	KindBundleVerification ErrorKind = "bundle_verification"
	// KindAmbiguousAttestations means more than one attestation satisfied a policy.
	KindAmbiguousAttestations ErrorKind = "ambiguous_attestations"
)

// Error is a typed verification error suitable for CLI rendering.
type Error struct {
	// Kind identifies the stable verification failure category.
	Kind ErrorKind
	// Message describes the concrete failure.
	Message string
	// Err wraps the underlying adapter or system error when one exists.
	Err error
}

// Error returns the human-readable verification error.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// IsKind reports whether err contains a verification error with kind.
func IsKind(err error, kind ErrorKind) bool {
	var verificationErr *Error
	if !errors.As(err, &verificationErr) {
		return false
	}
	return verificationErr.Kind == kind
}

func newError(kind ErrorKind, format string, args ...any) *Error {
	return &Error{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	}
}

func wrapError(kind ErrorKind, err error, format string, args ...any) *Error {
	return &Error{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
		Err:     err,
	}
}

func mismatch(kind ErrorKind, format string, args ...any) error {
	return newError(kind, format, args...)
}
