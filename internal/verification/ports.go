package verification

import "context"

// ReleaseResolution contains the resolved Git objects for one release tag.
type ReleaseResolution struct {
	// ReleaseTagDigest is the Git tag ref object digest used by GitHub release attestations.
	ReleaseTagDigest Digest
	// SourceDigest is the peeled source commit digest for provenance binding. Zero means unavailable.
	SourceDigest Digest
}

// ReleaseResolver resolves GitHub release tags to immutable Git object digests.
type ReleaseResolver interface {
	// ResolveReleaseTag resolves tag in repository to release and source digests.
	ResolveReleaseTag(ctx context.Context, repository Repository, tag ReleaseTag) (ReleaseResolution, error)
}

// AttestationSource loads attestations needed by core verification.
type AttestationSource interface {
	// FetchReleaseAttestations returns release attestations for a tag ref object digest.
	FetchReleaseAttestations(ctx context.Context, repository Repository, tagDigest Digest) ([]Attestation, error)
	// FetchProvenanceAttestations returns provenance attestations for an artifact digest.
	FetchProvenanceAttestations(
		ctx context.Context,
		repository Repository,
		artifactDigest Digest,
	) ([]Attestation, error)
}

// BundleVerifier verifies an attestation bundle and extracts trusted evidence.
type BundleVerifier interface {
	// Verify verifies attestation against expectedSubject and returns trusted timestamp-backed evidence.
	Verify(ctx context.Context, attestation Attestation, expectedSubject Digest) (VerifiedAttestation, error)
}

// Dependencies contains the ports consumed by Verifier.
type Dependencies struct {
	// ReleaseResolver resolves release tags.
	ReleaseResolver ReleaseResolver
	// AttestationSource fetches release and provenance attestations.
	AttestationSource AttestationSource
	// BundleVerifier verifies attestation bundles cryptographically.
	BundleVerifier BundleVerifier
	// ArtifactDigester computes local artifact digests. Nil selects SHA256FileDigester.
	ArtifactDigester ArtifactDigester
}

// Verifier orchestrates release and provenance verification.
type Verifier struct {
	releases     ReleaseResolver
	attestations AttestationSource
	bundles      BundleVerifier
	digester     ArtifactDigester
}

// NewVerifier creates a Verifier from the provided dependencies.
func NewVerifier(deps Dependencies) (*Verifier, error) {
	if deps.ReleaseResolver == nil {
		return nil, newError(KindInvalidRequest, "release resolver must be set")
	}
	if deps.AttestationSource == nil {
		return nil, newError(KindInvalidRequest, "attestation source must be set")
	}
	if deps.BundleVerifier == nil {
		return nil, newError(KindInvalidRequest, "bundle verifier must be set")
	}
	if deps.ArtifactDigester == nil {
		deps.ArtifactDigester = SHA256FileDigester{}
	}

	return &Verifier{
		releases:     deps.ReleaseResolver,
		attestations: deps.AttestationSource,
		bundles:      deps.BundleVerifier,
		digester:     deps.ArtifactDigester,
	}, nil
}
