package verification

const (
	// ReleasePredicateV01 is the GitHub immutable release attestation predicate type.
	ReleasePredicateV01 = "https://in-toto.io/attestation/release/v0.1"
	// SLSAPredicateV1 is the SLSA provenance predicate type required for artifacts.
	SLSAPredicateV1 = "https://slsa.dev/provenance/v1"
)

// Subject identifies one statement subject and its digest.
type Subject struct {
	// Name is the subject name from the attestation statement.
	Name string
	// Digest is the subject digest from the attestation statement.
	Digest Digest
}

// Statement contains the trusted statement fields needed by core policy.
type Statement struct {
	// PredicateType is the in-toto statement predicate type.
	PredicateType string
	// Subjects are the artifacts or refs covered by the statement.
	Subjects []Subject
	// Predicate contains predicate-specific fields used by core policy.
	Predicate Predicate
}

// Predicate contains predicate-specific fields used by core policy.
type Predicate struct {
	// ReleaseTag is the immutable release tag recorded by GitHub release attestations.
	ReleaseTag ReleaseTag
	// BuildType identifies the provenance build type when available.
	BuildType string
	// BuilderID identifies the provenance builder when available.
	BuilderID string
}

func (s Statement) hasSubjectDigest(digest Digest) bool {
	for _, subject := range s.Subjects {
		if subject.Digest.equal(digest) {
			return true
		}
	}
	return false
}
