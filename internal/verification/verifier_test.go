package verification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifierVerifyReleaseAssetSucceeds(t *testing.T) {
	tc := newTestContext(t)

	evidence, err := tc.verifier.VerifyReleaseAsset(context.Background(), tc.request)

	require.NoError(t, err)
	assert.Equal(t, tc.request.Repository, evidence.Repository)
	assert.Equal(t, tc.request.Tag, evidence.Tag)
	assert.Equal(t, tc.assetDigest, evidence.AssetDigest)
	assert.Equal(t, tc.releaseDigest, evidence.ReleaseTagDigest)
	assert.Equal(t, "release-attestation", evidence.ReleaseAttestation.AttestationID)
	assert.Equal(t, GitHubReleaseSubjectAlternativeName, evidence.ReleaseAttestation.SubjectAlternativeName)
	assert.Equal(t, "provenance-attestation", evidence.ProvenanceAttestation.AttestationID)
	assert.Equal(t, GitHubActionsOIDCIssuer, evidence.ProvenanceAttestation.Issuer)
	assert.Equal(t, tc.request.Repository, evidence.ProvenanceAttestation.SourceRepository)
	assert.Equal(t, WorkflowIdentity("https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main"), evidence.ProvenanceAttestation.SignerWorkflow)
	assert.Equal(t, tc.signerDigest, evidence.ProvenanceAttestation.SignerDigest)
	assert.NotEmpty(t, evidence.ReleaseAttestation.VerifiedTimestamps)
	assert.NotEmpty(t, evidence.ProvenanceAttestation.VerifiedTimestamps)

	require.Len(t, tc.bundle.calls, 2)
	assert.Equal(t, tc.releaseDigest, tc.bundle.calls[0].expectedSubject)
	assert.Equal(t, tc.assetDigest, tc.bundle.calls[1].expectedSubject)
}

func TestVerifierVerifyReleaseAssetHonorsPinnedSignerWorkflowRef(t *testing.T) {
	tc := newTestContext(t)
	tc.request.Policy.TrustedSignerWorkflow = "owner/repo/.github/workflows/release.yml@refs/heads/main"

	_, err := tc.verifier.VerifyReleaseAsset(context.Background(), tc.request)

	require.NoError(t, err)
}

func TestVerifierVerifyReleaseAssetHonorsPinnedSignerWorkflowDigest(t *testing.T) {
	tc := newTestContext(t)
	tc.request.Policy.ExpectedSignerDigest = tc.signerDigest

	_, err := tc.verifier.VerifyReleaseAsset(context.Background(), tc.request)

	require.NoError(t, err)
}

func TestVerifierVerifyReleaseAssetAcceptsCurrentReleasePredicate(t *testing.T) {
	tc := newTestContext(t)
	tc.updateResult("release-attestation", func(v *VerifiedAttestation) {
		v.Statement.PredicateType = ReleasePredicateV02
	})

	_, err := tc.verifier.VerifyReleaseAsset(context.Background(), tc.request)

	require.NoError(t, err)
}

func TestVerifierVerifyReleaseAssetFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*testContext)
		wantKind ErrorKind
	}{
		{
			name: "missing release attestation",
			mutate: func(tc *testContext) {
				tc.source.release = nil
			},
			wantKind: KindNoReleaseAttestation,
		},
		{
			name: "release missing timestamp evidence",
			mutate: func(tc *testContext) {
				tc.updateResult("release-attestation", func(v *VerifiedAttestation) {
					v.VerifiedTimestamps = nil
				})
			},
			wantKind: KindTimestampMissing,
		},
		{
			name: "release signer identity mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("release-attestation", func(v *VerifiedAttestation) {
					v.Certificate.SubjectAlternativeName = "https://github.com/owner/repo/.github/workflows/release.yml"
				})
			},
			wantKind: KindReleaseSignerMismatch,
		},
		{
			name: "release predicate mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("release-attestation", func(v *VerifiedAttestation) {
					v.Statement.PredicateType = SLSAPredicateV1
				})
			},
			wantKind: KindReleasePredicateMismatch,
		},
		{
			name: "release tag mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("release-attestation", func(v *VerifiedAttestation) {
					v.Statement.Predicate.ReleaseTag = "v9.9.9"
				})
			},
			wantKind: KindReleaseTagMismatch,
		},
		{
			name: "release subject digest mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("release-attestation", func(v *VerifiedAttestation) {
					v.Statement.Subjects = []Subject{{Name: "tag", Digest: tc.releaseDigest}}
				})
			},
			wantKind: KindReleaseSubjectMismatch,
		},
		{
			name: "missing provenance attestation",
			mutate: func(tc *testContext) {
				tc.source.provenance = nil
			},
			wantKind: KindNoProvenanceAttestation,
		},
		{
			name: "provenance missing timestamp evidence",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.VerifiedTimestamps = nil
				})
			},
			wantKind: KindTimestampMissing,
		},
		{
			name: "provenance predicate mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Statement.PredicateType = "https://spdx.dev/Document/v2.3"
				})
			},
			wantKind: KindProvenancePredicateMismatch,
		},
		{
			name: "provenance subject digest mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Statement.Subjects = []Subject{{Name: "other", Digest: mustDigest(t, "sha256", repeatHex("dd", 32))}}
				})
			},
			wantKind: KindProvenanceSubjectMismatch,
		},
		{
			name: "provenance issuer mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Certificate.Issuer = "https://example.com/issuer"
				})
			},
			wantKind: KindProvenanceIssuerMismatch,
		},
		{
			name: "provenance certificate identity mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Certificate.SubjectAlternativeName = "https://github.com/owner/repo/.github/workflows/other.yml@refs/heads/main"
				})
			},
			wantKind: KindProvenanceIdentityMismatch,
		},
		{
			name: "provenance certificate workflow path casing mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Certificate.SubjectAlternativeName = "https://github.com/OWNER/REPO/.github/workflows/Release.yml@refs/heads/main"
				})
			},
			wantKind: KindProvenanceIdentityMismatch,
		},
		{
			name: "source repository mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Certificate.SourceRepository = Repository{Owner: "other", Name: "repo"}
				})
			},
			wantKind: KindSourceRepositoryMismatch,
		},
		{
			name: "signer workflow mismatch",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Certificate.SignerWorkflow = "owner/repo/.github/workflows/other.yml"
				})
			},
			wantKind: KindSignerWorkflowMismatch,
		},
		{
			name: "signer workflow digest mismatch",
			mutate: func(tc *testContext) {
				tc.request.Policy.ExpectedSignerDigest = mustDigest(t, "sha1", repeatHex("ee", 20))
			},
			wantKind: KindSignerDigestMismatch,
		},
		{
			name: "provenance certificate identity ref mismatch",
			mutate: func(tc *testContext) {
				tc.request.Policy.TrustedSignerWorkflow = "owner/repo/.github/workflows/release.yml@refs/tags/v1.2.3"
			},
			wantKind: KindProvenanceIdentityMismatch,
		},
		{
			name: "source ref mismatch",
			mutate: func(tc *testContext) {
				tc.request.Policy.ExpectedSourceRef = "refs/heads/main"
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Certificate.SourceRef = "refs/heads/dev"
				})
			},
			wantKind: KindSourceRefMismatch,
		},
		{
			name: "source digest mismatch",
			mutate: func(tc *testContext) {
				tc.request.Policy.ExpectedSourceDigest = mustDigest(t, "sha1", repeatHex("ee", 20))
			},
			wantKind: KindSourceDigestMismatch,
		},
		{
			name: "self-hosted runner",
			mutate: func(tc *testContext) {
				tc.updateResult("provenance-attestation", func(v *VerifiedAttestation) {
					v.Certificate.RunnerEnvironment = "self-hosted"
				})
			},
			wantKind: KindRunnerEnvironmentMismatch,
		},
		{
			name: "verifier error",
			mutate: func(tc *testContext) {
				tc.bundle.errors["provenance-attestation"] = errors.New("bad signature")
			},
			wantKind: KindBundleVerification,
		},
		{
			name: "ambiguous provenance attestations",
			mutate: func(tc *testContext) {
				duplicate := Attestation{ID: "provenance-attestation-duplicate"}
				tc.source.provenance = append(tc.source.provenance, duplicate)
				verified := tc.bundle.results["provenance-attestation"]
				verified.Attestation = duplicate
				tc.bundle.results[duplicate.ID] = verified
			},
			wantKind: KindAmbiguousAttestations,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tc := newTestContext(t)
			tt.mutate(tc)

			_, err := tc.verifier.VerifyReleaseAsset(context.Background(), tc.request)

			require.Error(t, err)
			assert.True(t, IsKind(err, tt.wantKind), "expected kind %s, got %v", tt.wantKind, err)
		})
	}
}

func TestVerifierVerifyReleaseAssetValidatesRequest(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*testContext)
		wantKind ErrorKind
	}{
		{
			name: "missing repository",
			mutate: func(tc *testContext) {
				tc.request.Repository = Repository{}
			},
			wantKind: KindInvalidRequest,
		},
		{
			name: "missing tag",
			mutate: func(tc *testContext) {
				tc.request.Tag = ""
			},
			wantKind: KindInvalidRequest,
		},
		{
			name: "missing asset path",
			mutate: func(tc *testContext) {
				tc.request.AssetPath = ""
			},
			wantKind: KindInvalidRequest,
		},
		{
			name: "artifact digest failure",
			mutate: func(tc *testContext) {
				tc.digester.err = errors.New("read failed")
			},
			wantKind: KindDigest,
		},
		{
			name: "invalid artifact digest from digester",
			mutate: func(tc *testContext) {
				tc.digester.digest = Digest{Algorithm: "sha256", Hex: "aa"}
			},
			wantKind: KindDigest,
		},
		{
			name: "invalid release digest from resolver",
			mutate: func(tc *testContext) {
				tc.resolver.digest = Digest{}
			},
			wantKind: KindResolveRelease,
		},
		{
			name: "missing trusted workflow",
			mutate: func(tc *testContext) {
				tc.request.Policy.TrustedSignerWorkflow = ""
			},
			wantKind: KindInvalidRequest,
		},
		{
			name: "invalid signer digest policy",
			mutate: func(tc *testContext) {
				tc.request.Policy.ExpectedSignerDigest = Digest{Algorithm: "sha1", Hex: "aa"}
			},
			wantKind: KindDigest,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tc := newTestContext(t)
			tt.mutate(tc)

			_, err := tc.verifier.VerifyReleaseAsset(context.Background(), tc.request)

			require.Error(t, err)
			assert.True(t, IsKind(err, tt.wantKind), "expected kind %s, got %v", tt.wantKind, err)
		})
	}
}

func TestNewVerifierRequiresCorePorts(t *testing.T) {
	tests := []struct {
		name   string
		deps   Dependencies
		assert func(*testing.T, error)
	}{
		{
			name: "release resolver",
			deps: Dependencies{
				AttestationSource: &fakeAttestationSource{},
				BundleVerifier:    &fakeBundleVerifier{},
			},
			assert: func(t *testing.T, err error) {
				assert.True(t, IsKind(err, KindInvalidRequest))
			},
		},
		{
			name: "attestation source",
			deps: Dependencies{
				ReleaseResolver: &fakeReleaseResolver{},
				BundleVerifier:  &fakeBundleVerifier{},
			},
			assert: func(t *testing.T, err error) {
				assert.True(t, IsKind(err, KindInvalidRequest))
			},
		},
		{
			name: "bundle verifier",
			deps: Dependencies{
				ReleaseResolver:   &fakeReleaseResolver{},
				AttestationSource: &fakeAttestationSource{},
			},
			assert: func(t *testing.T, err error) {
				assert.True(t, IsKind(err, KindInvalidRequest))
			},
		},
		{
			name: "default digester",
			deps: Dependencies{
				ReleaseResolver:   &fakeReleaseResolver{},
				AttestationSource: &fakeAttestationSource{},
				BundleVerifier:    &fakeBundleVerifier{},
			},
			assert: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVerifier(tt.deps)
			tt.assert(t, err)
		})
	}
}

type testContext struct {
	request       Request
	assetDigest   Digest
	releaseDigest Digest
	signerDigest  Digest
	resolver      *fakeReleaseResolver
	source        *fakeAttestationSource
	bundle        *fakeBundleVerifier
	digester      *fakeDigester
	verifier      *Verifier
}

func newTestContext(t *testing.T) *testContext {
	t.Helper()

	assetDigest := mustDigest(t, "sha256", repeatHex("aa", 32))
	releaseDigest := mustDigest(t, "sha1", repeatHex("bb", 20))
	sourceDigest := mustDigest(t, "sha1", repeatHex("cc", 20))
	signerDigest := mustDigest(t, "sha1", repeatHex("dd", 20))
	repository := Repository{Owner: "owner", Name: "repo"}
	tag := ReleaseTag("v1.2.3")
	verifiedTimestamps := []VerifiedTimestamp{{Kind: "signed-timestamp", Time: time.Unix(1700000000, 0)}}

	releaseAttestation := Attestation{ID: "release-attestation"}
	provenanceAttestation := Attestation{ID: "provenance-attestation"}
	source := &fakeAttestationSource{
		release:    []Attestation{releaseAttestation},
		provenance: []Attestation{provenanceAttestation},
	}
	bundle := &fakeBundleVerifier{
		results: map[string]VerifiedAttestation{
			releaseAttestation.ID: {
				Attestation: releaseAttestation,
				Certificate: CertificateEvidence{
					SubjectAlternativeName: GitHubReleaseSubjectAlternativeName,
				},
				VerifiedTimestamps: verifiedTimestamps,
				Statement: Statement{
					PredicateType: ReleasePredicateV01,
					Subjects: []Subject{
						{Name: "tag", Digest: releaseDigest},
						{Name: "artifact.tar.gz", Digest: assetDigest},
					},
					Predicate: Predicate{ReleaseTag: tag},
				},
			},
			provenanceAttestation.ID: {
				Attestation: provenanceAttestation,
				Statement: Statement{
					PredicateType: SLSAPredicateV1,
					Subjects:      []Subject{{Name: "artifact.tar.gz", Digest: assetDigest}},
				},
				Certificate: CertificateEvidence{
					Issuer:                 GitHubActionsOIDCIssuer,
					SubjectAlternativeName: "https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main",
					SourceRepository:       repository,
					SourceRef:              "refs/tags/v1.2.3",
					SourceDigest:           sourceDigest,
					SignerWorkflow:         "https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main",
					SignerDigest:           signerDigest,
					RunnerEnvironment:      RunnerEnvironmentGitHubHosted,
				},
				VerifiedTimestamps: verifiedTimestamps,
			},
		},
		errors: map[string]error{},
	}
	digester := &fakeDigester{digest: assetDigest}
	resolver := &fakeReleaseResolver{digest: releaseDigest}

	verifier, err := NewVerifier(Dependencies{
		ReleaseResolver:   resolver,
		AttestationSource: source,
		BundleVerifier:    bundle,
		ArtifactDigester:  digester,
	})
	require.NoError(t, err)

	return &testContext{
		request: Request{
			Repository: repository,
			Tag:        tag,
			AssetPath:  "/tmp/artifact.tar.gz",
			Policy: Policy{
				TrustedSignerWorkflow: "owner/repo/.github/workflows/release.yml",
			},
		},
		assetDigest:   assetDigest,
		releaseDigest: releaseDigest,
		signerDigest:  signerDigest,
		resolver:      resolver,
		source:        source,
		bundle:        bundle,
		digester:      digester,
		verifier:      verifier,
	}
}

func (tc *testContext) updateResult(id string, update func(*VerifiedAttestation)) {
	verified := tc.bundle.results[id]
	update(&verified)
	tc.bundle.results[id] = verified
}

type fakeReleaseResolver struct {
	digest Digest
	err    error
}

func (f *fakeReleaseResolver) ResolveReleaseTag(context.Context, Repository, ReleaseTag) (Digest, error) {
	return f.digest, f.err
}

type fakeAttestationSource struct {
	release       []Attestation
	releaseErr    error
	provenance    []Attestation
	provenanceErr error
}

func (f *fakeAttestationSource) FetchReleaseAttestations(context.Context, Repository, Digest) ([]Attestation, error) {
	return f.release, f.releaseErr
}

func (f *fakeAttestationSource) FetchProvenanceAttestations(context.Context, Repository, Digest) ([]Attestation, error) {
	return f.provenance, f.provenanceErr
}

type verifyCall struct {
	attestation     Attestation
	expectedSubject Digest
}

type fakeBundleVerifier struct {
	results map[string]VerifiedAttestation
	errors  map[string]error
	calls   []verifyCall
}

func (f *fakeBundleVerifier) Verify(_ context.Context, attestation Attestation, expectedSubject Digest) (VerifiedAttestation, error) {
	f.calls = append(f.calls, verifyCall{attestation: attestation, expectedSubject: expectedSubject})
	if err := f.errors[attestation.ID]; err != nil {
		return VerifiedAttestation{}, err
	}
	return f.results[attestation.ID], nil
}

type fakeDigester struct {
	digest Digest
	err    error
}

func (f *fakeDigester) DigestFile(string) (Digest, error) {
	if f.err != nil {
		return Digest{}, f.err
	}
	return f.digest, nil
}

func mustDigest(t *testing.T, algorithm string, value string) Digest {
	t.Helper()
	digest, err := NewDigest(algorithm, value)
	require.NoError(t, err)
	return digest
}

func repeatHex(value string, count int) string {
	var out string
	for range count {
		out += value
	}
	return out
}
