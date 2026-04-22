package verification

// Evidence contains the data needed to record verification.json later.
type Evidence struct {
	// Repository is the verified GitHub repository.
	Repository Repository
	// Tag is the verified release tag.
	Tag ReleaseTag
	// AssetDigest is the verified local asset digest.
	AssetDigest Digest
	// ReleaseTagDigest is the digest of the Git tag ref object resolved for the release.
	ReleaseTagDigest Digest
	// ReleaseAttestation records the accepted immutable release attestation.
	ReleaseAttestation AttestationEvidence
	// ProvenanceAttestation records the accepted SLSA provenance attestation.
	ProvenanceAttestation AttestationEvidence
}

// AttestationEvidence summarizes one accepted verified attestation.
type AttestationEvidence struct {
	// AttestationID identifies the accepted attestation.
	AttestationID string
	// PredicateType is the accepted statement predicate type.
	PredicateType string
	// Issuer is the accepted OIDC issuer.
	Issuer string
	// SubjectAlternativeName is the accepted signing certificate SAN.
	SubjectAlternativeName string
	// SignerWorkflow is the accepted workflow identity.
	SignerWorkflow WorkflowIdentity
	// SignerDigest is the accepted signer workflow digest.
	SignerDigest Digest
	// SourceRepository is the accepted source repository.
	SourceRepository Repository
	// SourceRef is the accepted source ref.
	SourceRef string
	// SourceDigest is the accepted source digest.
	SourceDigest Digest
	// RunnerEnvironment is the accepted runner environment.
	RunnerEnvironment RunnerEnvironment
	// VerifiedTimestamps contains accepted timestamp evidence.
	VerifiedTimestamps []VerifiedTimestamp
}

func evidenceFor(verified VerifiedAttestation) AttestationEvidence {
	return AttestationEvidence{
		AttestationID:          verified.Attestation.ID,
		PredicateType:          verified.Statement.PredicateType,
		Issuer:                 verified.Certificate.Issuer,
		SubjectAlternativeName: verified.Certificate.SubjectAlternativeName,
		SignerWorkflow:         verified.Certificate.SignerWorkflow,
		SignerDigest:           verified.Certificate.SignerDigest,
		SourceRepository:       verified.Certificate.SourceRepository,
		SourceRef:              verified.Certificate.SourceRef,
		SourceDigest:           verified.Certificate.SourceDigest,
		RunnerEnvironment:      verified.Certificate.RunnerEnvironment,
		VerifiedTimestamps:     verified.VerifiedTimestamps,
	}
}
