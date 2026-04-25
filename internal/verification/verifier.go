package verification

import "context"

// VerifyReleaseAsset verifies one downloaded GitHub release asset.
func (v *Verifier) VerifyReleaseAsset(ctx context.Context, request Request) (Evidence, error) {
	request = request.withDefaults()
	// Require enough caller-supplied context to bind the local file to one repository, tag, and policy.
	if err := request.validate(); err != nil {
		return Evidence{}, err
	}

	assetDigest, err := v.digester.DigestFile(request.AssetPath)
	if err != nil {
		return Evidence{}, wrapError(KindDigest, err, "digest artifact %s", request.AssetPath)
	}
	// Only supported, correctly sized digests may become attestation lookup keys or recorded evidence.
	if err := assetDigest.validate(); err != nil {
		return Evidence{}, wrapError(KindDigest, err, "digest artifact %s returned invalid digest", request.AssetPath)
	}

	releaseResolution, err := v.releases.ResolveReleaseTag(ctx, request.Repository, request.Tag)
	if err != nil {
		return Evidence{}, wrapError(KindResolveRelease, err, "resolve release tag %s", request.Tag)
	}
	releaseDigest := releaseResolution.ReleaseTagDigest
	// The resolved tag digest is the subject for GitHub's immutable release attestation.
	if err := releaseDigest.validate(); err != nil {
		return Evidence{}, wrapError(KindResolveRelease, err, "resolve release tag %s returned invalid digest", request.Tag)
	}
	if !releaseResolution.SourceDigest.IsZero() {
		if err := releaseResolution.SourceDigest.validate(); err != nil {
			return Evidence{}, wrapError(KindResolveRelease, err, "resolve release tag %s returned invalid source digest", request.Tag)
		}
	}

	releaseAttestations, err := v.attestations.FetchReleaseAttestations(ctx, request.Repository, releaseDigest)
	if err != nil {
		return Evidence{}, wrapError(KindFetchReleaseAttestations, err, "fetch release attestations for %s", releaseDigest)
	}
	releaseAttestation, err := v.verifyReleaseAttestation(ctx, releaseAttestations, releaseDigest, assetDigest, request.Tag)
	if err != nil {
		return Evidence{}, err
	}

	provenanceAttestations, err := v.attestations.FetchProvenanceAttestations(ctx, request.Repository, assetDigest)
	if err != nil {
		return Evidence{}, wrapError(KindFetchProvenanceAttestations, err, "fetch provenance attestations for %s", assetDigest)
	}
	policy := request.Policy
	if policy.ExpectedSourceRef.IsZero() {
		policy.ExpectedSourceRef = request.Tag.RefName()
	}
	if policy.ExpectedSourceDigest.IsZero() && !releaseResolution.SourceDigest.IsZero() {
		policy.ExpectedSourceDigest = releaseResolution.SourceDigest
	}
	provenance, err := v.verifyProvenanceAttestation(ctx, provenanceAttestations, assetDigest, policy)
	if err != nil {
		return Evidence{}, err
	}

	return Evidence{
		Repository:            request.Repository,
		Tag:                   request.Tag,
		AssetDigest:           assetDigest,
		ReleaseTagDigest:      releaseDigest,
		ReleaseAttestation:    evidenceFor(releaseAttestation),
		ProvenanceAttestation: evidenceFor(provenance),
	}, nil
}

func (v *Verifier) verifyReleaseAttestation(ctx context.Context, attestations []Attestation, releaseDigest Digest, assetDigest Digest, tag ReleaseTag) (VerifiedAttestation, error) {
	// GitHub must provide at least one release attestation for the resolved tag digest.
	if len(attestations) == 0 {
		return VerifiedAttestation{}, newError(KindNoReleaseAttestation, "no release attestations found for %s", releaseDigest)
	}

	matches := make([]VerifiedAttestation, 0, 1)
	var fallback error
	var verifiedCount int
	for _, attestation := range attestations {
		verified, err := v.bundles.Verify(ctx, attestation, releaseDigest)
		if err != nil {
			fallback = wrapError(KindBundleVerification, err, "verify release attestation %s", attestation.ID)
			continue
		}
		verifiedCount++

		// Sigstore verification must include independent timestamp evidence for the bundle.
		if !verified.hasTimestampEvidence() {
			fallback = mismatch(KindTimestampMissing, "release attestation %s has no trusted timestamp evidence", attestation.ID)
			continue
		}
		// Release attestations must be signed by GitHub's release attestation identity, not a workflow.
		if !verified.Certificate.hasGitHubReleaseIdentity() {
			fallback = mismatch(KindReleaseSignerMismatch, "release attestation %s has signer identity %q, not %q", attestation.ID, verified.Certificate.SubjectAlternativeName, GitHubReleaseSubjectAlternativeName)
			continue
		}
		// The release statement must use GitHub's immutable release predicate, not provenance or another claim type.
		if !verified.Statement.hasReleasePredicate() {
			fallback = mismatch(KindReleasePredicateMismatch, "release attestation %s has predicate %q", attestation.ID, verified.Statement.PredicateType)
			continue
		}
		// The release attestation must describe the exact tag requested for installation.
		if verified.Statement.Predicate.ReleaseTag != tag {
			fallback = mismatch(KindReleaseTagMismatch, "release attestation %s is for tag %s, not %s", attestation.ID, verified.Statement.Predicate.ReleaseTag, tag)
			continue
		}
		// The local bytes must appear as a subject in the immutable release record.
		if !verified.Statement.hasSubjectDigest(assetDigest) {
			fallback = mismatch(KindReleaseSubjectMismatch, "release attestation %s does not contain asset digest %s", attestation.ID, assetDigest)
			continue
		}

		matches = append(matches, verified)
	}

	return exactlyOne(matches, fallback, verifiedCount, KindNoReleaseAttestation, "release attestation")
}

func (v *Verifier) verifyProvenanceAttestation(ctx context.Context, attestations []Attestation, assetDigest Digest, policy Policy) (VerifiedAttestation, error) {
	// GitHub must provide at least one provenance attestation for the local asset digest.
	if len(attestations) == 0 {
		return VerifiedAttestation{}, newError(KindNoProvenanceAttestation, "no provenance attestations found for %s", assetDigest)
	}

	matches := make([]VerifiedAttestation, 0, 1)
	var fallback error
	var verifiedCount int
	for _, attestation := range attestations {
		verified, err := v.bundles.Verify(ctx, attestation, assetDigest)
		if err != nil {
			fallback = wrapError(KindBundleVerification, err, "verify provenance attestation %s", attestation.ID)
			continue
		}
		verifiedCount++

		// Sigstore verification must include independent timestamp evidence for the bundle.
		if !verified.hasTimestampEvidence() {
			fallback = mismatch(KindTimestampMissing, "provenance attestation %s has no trusted timestamp evidence", attestation.ID)
			continue
		}
		// Artifact provenance must use the SLSA v1 predicate.
		if verified.Statement.PredicateType != SLSAPredicateV1 {
			fallback = mismatch(KindProvenancePredicateMismatch, "provenance attestation %s has predicate %q", attestation.ID, verified.Statement.PredicateType)
			continue
		}
		// The provenance statement must describe the local bytes being installed.
		if !verified.Statement.hasSubjectDigest(assetDigest) {
			fallback = mismatch(KindProvenanceSubjectMismatch, "provenance attestation %s does not contain asset digest %s", attestation.ID, assetDigest)
			continue
		}
		// GitHub Actions OIDC must be the certificate issuer for build provenance.
		if !verified.Certificate.hasGitHubActionsIssuer() {
			fallback = mismatch(KindProvenanceIssuerMismatch, "provenance attestation %s has OIDC issuer %q, not %q", attestation.ID, verified.Certificate.Issuer, GitHubActionsOIDCIssuer)
			continue
		}
		// The certificate SAN is the non-forgeable workflow identity and must match the trusted workflow.
		subjectWorkflow, err := NewWorkflowIdentity(verified.Certificate.SubjectAlternativeName)
		if err != nil {
			fallback = mismatch(KindProvenanceIdentityMismatch, "provenance attestation %s has invalid certificate identity %q", attestation.ID, verified.Certificate.SubjectAlternativeName)
			continue
		}
		if !policy.TrustedSignerWorkflow.matches(subjectWorkflow) {
			fallback = mismatch(KindProvenanceIdentityMismatch, "provenance attestation %s has certificate identity %q, not %s", attestation.ID, verified.Certificate.SubjectAlternativeName, policy.TrustedSignerWorkflow)
			continue
		}
		// The certificate must bind provenance to the expected source repository.
		if !verified.Certificate.SourceRepository.Equal(policy.ExpectedSourceRepository) {
			fallback = mismatch(KindSourceRepositoryMismatch, "provenance source repository is %s, not %s", verified.Certificate.SourceRepository, policy.ExpectedSourceRepository)
			continue
		}
		// The signer workflow extension should corroborate the certificate SAN.
		if !policy.TrustedSignerWorkflow.matches(verified.Certificate.SignerWorkflow) {
			fallback = mismatch(KindSignerWorkflowMismatch, "provenance signer workflow is %s, not %s", verified.Certificate.SignerWorkflow, policy.TrustedSignerWorkflow)
			continue
		}
		// If configured, require provenance to come from the exact expected signer workflow digest.
		if !policy.ExpectedSignerDigest.IsZero() && !verified.Certificate.SignerDigest.equal(policy.ExpectedSignerDigest) {
			fallback = mismatch(KindSignerDigestMismatch, "provenance signer digest is %s, not %s", verified.Certificate.SignerDigest, policy.ExpectedSignerDigest)
			continue
		}
		// If configured, require provenance to come from the exact expected source ref.
		if !policy.ExpectedSourceRef.IsZero() && verified.Certificate.SourceRef != policy.ExpectedSourceRef {
			fallback = mismatch(KindSourceRefMismatch, "provenance source ref is %s, not %s", verified.Certificate.SourceRef, policy.ExpectedSourceRef)
			continue
		}
		// If configured, require provenance to come from the exact expected source digest.
		if !policy.ExpectedSourceDigest.IsZero() && !verified.Certificate.SourceDigest.equal(policy.ExpectedSourceDigest) {
			fallback = mismatch(KindSourceDigestMismatch, "provenance source digest is %s, not %s", verified.Certificate.SourceDigest, policy.ExpectedSourceDigest)
			continue
		}
		// Self-hosted runners are excluded from the initial trust policy.
		if verified.Certificate.RunnerEnvironment != RunnerEnvironmentGitHubHosted {
			fallback = mismatch(KindRunnerEnvironmentMismatch, "provenance runner environment is %s, not %s", verified.Certificate.RunnerEnvironment, RunnerEnvironmentGitHubHosted)
			continue
		}

		matches = append(matches, verified)
	}

	return exactlyOne(matches, fallback, verifiedCount, KindNoProvenanceAttestation, "provenance attestation")
}

func exactlyOne(matches []VerifiedAttestation, fallback error, verifiedCount int, emptyKind ErrorKind, label string) (VerifiedAttestation, error) {
	// Verification evidence must point to one unambiguous attestation for each phase.
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		if fallback != nil {
			return VerifiedAttestation{}, fallback
		}
		if verifiedCount == 0 {
			return VerifiedAttestation{}, newError(KindBundleVerification, "no %s bundles could be verified", label)
		}
		return VerifiedAttestation{}, newError(emptyKind, "no accepted %s found", label)
	default:
		return VerifiedAttestation{}, newError(KindAmbiguousAttestations, "expected one accepted %s, got %d", label, len(matches))
	}
}
