package sigstore

import (
	"context"
	"errors"
	"testing"
	"time"

	intoto "github.com/in-toto/attestation/go/v1"
	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	sigverify "github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/meigma/ghd/internal/verification"
)

func TestVerifierVerifyConvertsSigstoreResult(t *testing.T) {
	core := &fakeSignedEntityVerifier{result: successfulResult(t)}
	adapter := newVerifierWithCore(core)
	expectedSubject := mustDigest(t, "sha256", repeatHex("aa", 32))

	verified, err := adapter.Verify(context.Background(), verification.Attestation{
		ID:     "attestation-1",
		Bundle: &sigbundle.Bundle{},
	}, expectedSubject)

	require.NoError(t, err)
	assert.Equal(t, "attestation-1", verified.Attestation.ID)
	assert.Equal(t, verification.SLSAPredicateV1, verified.Statement.PredicateType)
	require.Len(t, verified.Statement.Subjects, 1)
	assert.Equal(t, expectedSubject, verified.Statement.Subjects[0].Digest)
	assert.Equal(t, "https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main", verified.Certificate.SubjectAlternativeName)
	assert.Equal(t, verification.GitHubActionsOIDCIssuer, verified.Certificate.Issuer)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, verified.Certificate.SourceRepository)
	assert.Equal(t, verification.SourceRef("refs/tags/v1.2.3"), verified.Certificate.SourceRef)
	assert.Equal(t, mustDigest(t, "sha1", repeatHex("bb", 20)), verified.Certificate.SourceDigest)
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml@refs/heads/main"), verified.Certificate.SignerWorkflow)
	assert.Equal(t, mustDigest(t, "sha1", repeatHex("cc", 20)), verified.Certificate.SignerDigest)
	assert.Equal(t, verification.RunnerEnvironmentGitHubHosted, verified.Certificate.RunnerEnvironment)
	require.Len(t, verified.VerifiedTimestamps, 1)
	assert.Equal(t, "signed-timestamp", verified.VerifiedTimestamps[0].Kind)
	assert.Equal(t, 1, core.calls)
}

func TestVerifierRoutesBundlesByIssuer(t *testing.T) {
	githubCore := &fakeSignedEntityVerifier{result: successfulResult(t)}
	publicGoodCore := &fakeSignedEntityVerifier{result: successfulResult(t)}
	issuer := GitHubIssuerOrg
	adapter := &Verifier{
		github:     githubCore,
		publicGood: publicGoodCore,
		custom:     map[string]signedEntityVerifier{},
		bundleIssuer: func(*sigbundle.Bundle) (string, error) {
			return issuer, nil
		},
	}
	attestation := verification.Attestation{ID: "attestation-1", Bundle: &sigbundle.Bundle{}}
	subject := mustDigest(t, "sha256", repeatHex("aa", 32))

	_, err := adapter.Verify(context.Background(), attestation, subject)
	require.NoError(t, err)

	issuer = PublicGoodIssuerOrg
	_, err = adapter.Verify(context.Background(), attestation, subject)
	require.NoError(t, err)

	assert.Equal(t, 1, githubCore.calls)
	assert.Equal(t, 1, publicGoodCore.calls)
}

func TestVerifierFailsWhenIssuerVerifierIsMissing(t *testing.T) {
	adapter := &Verifier{
		github: &fakeSignedEntityVerifier{result: successfulResult(t)},
		custom: map[string]signedEntityVerifier{},
		bundleIssuer: func(*sigbundle.Bundle) (string, error) {
			return PublicGoodIssuerOrg, nil
		},
	}

	_, err := adapter.Verify(context.Background(), verification.Attestation{
		ID:     "attestation-1",
		Bundle: &sigbundle.Bundle{},
	}, mustDigest(t, "sha256", repeatHex("aa", 32)))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Public Good")
}

func TestVerifierVerifyFailsClosed(t *testing.T) {
	tests := []struct {
		name        string
		attestation verification.Attestation
		result      *sigverify.VerificationResult
		err         error
	}{
		{
			name: "wrong bundle type",
			attestation: verification.Attestation{
				ID:     "attestation-1",
				Bundle: "not-a-bundle",
			},
		},
		{
			name: "sigstore verification error",
			attestation: verification.Attestation{
				ID:     "attestation-1",
				Bundle: &sigbundle.Bundle{},
			},
			err: errors.New("bad signature"),
		},
		{
			name: "missing timestamp evidence",
			attestation: verification.Attestation{
				ID:     "attestation-1",
				Bundle: &sigbundle.Bundle{},
			},
			result: mutateResult(t, func(result *sigverify.VerificationResult) {
				result.VerifiedTimestamps = nil
			}),
		},
		{
			name: "missing certificate evidence",
			attestation: verification.Attestation{
				ID:     "attestation-1",
				Bundle: &sigbundle.Bundle{},
			},
			result: mutateResult(t, func(result *sigverify.VerificationResult) {
				result.Signature.Certificate = nil
			}),
		},
		{
			name: "invalid subject digest",
			attestation: verification.Attestation{
				ID:     "attestation-1",
				Bundle: &sigbundle.Bundle{},
			},
			result: mutateResult(t, func(result *sigverify.VerificationResult) {
				result.Statement.Subject[0].Digest["sha256"] = "aa"
			}),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			adapter := newVerifierWithCore(&fakeSignedEntityVerifier{result: tt.result, err: tt.err})

			_, err := adapter.Verify(context.Background(), tt.attestation, mustDigest(t, "sha256", repeatHex("aa", 32)))

			require.Error(t, err)
		})
	}
}

func TestNewVerifierRequiresTrustConfiguration(t *testing.T) {
	_, err := NewVerifier()
	require.Error(t, err)

	_, err = NewVerifier(WithTrustedRootJSON([]byte("{")))
	require.Error(t, err)
}

type fakeSignedEntityVerifier struct {
	result *sigverify.VerificationResult
	err    error
	calls  int
}

func (f *fakeSignedEntityVerifier) Verify(_ sigverify.SignedEntity, policy sigverify.PolicyBuilder) (*sigverify.VerificationResult, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	_, err := policy.BuildConfig()
	if err != nil {
		return nil, err
	}
	return f.result, nil
}

func successfulResult(t *testing.T) *sigverify.VerificationResult {
	t.Helper()
	predicate, err := structpb.NewStruct(map[string]any{
		"buildDefinition": map[string]any{
			"buildType": "https://github.com/actions",
		},
		"runDetails": map[string]any{
			"builder": map[string]any{
				"id": "https://github.com/actions/runner",
			},
		},
		"tag": "v1.2.3",
	})
	require.NoError(t, err)

	return &sigverify.VerificationResult{
		Statement: &intoto.Statement{
			PredicateType: verification.SLSAPredicateV1,
			Subject: []*intoto.ResourceDescriptor{
				{
					Name:   "artifact.tar.gz",
					Digest: map[string]string{"sha256": repeatHex("aa", 32)},
				},
			},
			Predicate: predicate,
		},
		Signature: &sigverify.SignatureVerificationResult{
			Certificate: &certificate.Summary{
				SubjectAlternativeName: "https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main",
				Extensions: certificate.Extensions{
					Issuer:                 verification.GitHubActionsOIDCIssuer,
					SourceRepositoryURI:    "https://github.com/owner/repo",
					SourceRepositoryRef:    "refs/tags/v1.2.3",
					SourceRepositoryDigest: repeatHex("bb", 20),
					BuildSignerURI:         "https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main",
					BuildSignerDigest:      repeatHex("cc", 20),
					RunnerEnvironment:      string(verification.RunnerEnvironmentGitHubHosted),
				},
			},
		},
		VerifiedTimestamps: []sigverify.TimestampVerificationResult{
			{Type: "signed-timestamp", Timestamp: time.Unix(1700000000, 0)},
		},
	}
}

func mutateResult(t *testing.T, mutate func(*sigverify.VerificationResult)) *sigverify.VerificationResult {
	t.Helper()
	result := successfulResult(t)
	mutate(result)
	return result
}

func mustDigest(t *testing.T, algorithm string, value string) verification.Digest {
	t.Helper()
	digest, err := verification.NewDigest(algorithm, value)
	require.NoError(t, err)
	return digest
}

func repeatHex(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}
