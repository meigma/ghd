package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/klauspost/compress/snappy"
	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	sigdata "github.com/sigstore/sigstore-go/pkg/testing/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/verification"
)

func TestClientResolveReleaseTagUsesGitHubRefObjectSHA(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		assert.Equal(t, "/repos/OWNER/repo/git/ref/tags/release/v1.2.3", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"object":{"sha":%q}}`, repeatHex("aa", 20))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithToken("token-123"), WithUserAgent("ghd-test"))

	digest, err := client.ResolveReleaseTag(context.Background(), verification.Repository{Owner: "OWNER", Name: "repo"}, "release/v1.2.3")

	require.NoError(t, err)
	assert.Equal(t, "sha1", digest.Algorithm)
	assert.Equal(t, repeatHex("aa", 20), digest.Hex)
	assert.Equal(t, "application/vnd.github+json", gotHeader.Get("Accept"))
	assert.Equal(t, DefaultAPIVersion, gotHeader.Get("X-GitHub-Api-Version"))
	assert.Equal(t, "Bearer token-123", gotHeader.Get("Authorization"))
	assert.Equal(t, "ghd-test", gotHeader.Get("User-Agent"))
}

func TestClientResolveReleaseTagEscapesSpecialCharactersOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/git/ref/tags/v%231%251", r.URL.EscapedPath())
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"object":{"sha":%q}}`, repeatHex("ab", 20))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	digest, err := client.ResolveReleaseTag(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, "v#1%1")

	require.NoError(t, err)
	assert.Equal(t, repeatHex("ab", 20), digest.Hex)
}

func TestClientFetchReleaseAttestationsFetchesBundleURLs(t *testing.T) {
	bundleBytes := compressedBundle(t)
	var apiHeader http.Header
	var bundleHeader http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/attestations/sha1:" + repeatHex("bb", 20):
			apiHeader = r.Header.Clone()
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))
			assert.Equal(t, "release", r.URL.Query().Get("predicate_type"))
			fmt.Fprintf(w, `{"attestations":[{"id":"release-1","bundle_url":%q}]}`, "http://"+r.Host+"/bundle/release")
		case "/bundle/release":
			bundleHeader = r.Header.Clone()
			w.Write(bundleBytes)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithToken("token-123"))
	digest := mustDigest(t, "sha1", repeatHex("bb", 20))

	attestations, err := client.FetchReleaseAttestations(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, digest)

	require.NoError(t, err)
	require.Len(t, attestations, 1)
	assert.Equal(t, "release-1", attestations[0].ID)
	assert.IsType(t, &sigbundle.Bundle{}, attestations[0].Bundle)
	assert.Equal(t, "Bearer token-123", apiHeader.Get("Authorization"))
	assert.Empty(t, bundleHeader.Get("Authorization"), "bundle_url requests must not receive the GitHub token")
}

func TestClientFetchProvenanceAttestationsUsesProvenancePredicate(t *testing.T) {
	bundleBytes := compressedBundle(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/attestations/sha256:" + repeatHex("cc", 32):
			assert.Equal(t, "provenance", r.URL.Query().Get("predicate_type"))
			fmt.Fprintf(w, `{"attestations":[{"bundle_url":%q}]}`, "http://"+r.Host+"/bundle/provenance")
		case "/bundle/provenance":
			w.Write(bundleBytes)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	digest := mustDigest(t, "sha256", repeatHex("cc", 32))

	attestations, err := client.FetchProvenanceAttestations(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, digest)

	require.NoError(t, err)
	require.Len(t, attestations, 1)
	assert.Contains(t, attestations[0].ID, "/bundle/provenance")
}

func TestClientFetchAttestationsFollowsPagination(t *testing.T) {
	bundleBytes := compressedBundle(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/attestations/sha256:" + repeatHex("dd", 32):
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprintf(w, `{"attestations":[{"id":"second","bundle_url":%q}]}`, "http://"+r.Host+"/bundle/second")
				return
			}
			next := "http://" + r.Host + r.URL.Path + "?" + r.URL.RawQuery + "&page=2"
			w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next))
			fmt.Fprintf(w, `{"attestations":[{"id":"first","bundle_url":%q}]}`, "http://"+r.Host+"/bundle/first")
		case "/bundle/first", "/bundle/second":
			w.Write(bundleBytes)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	digest := mustDigest(t, "sha256", repeatHex("dd", 32))

	attestations, err := client.FetchProvenanceAttestations(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, digest)

	require.NoError(t, err)
	require.Len(t, attestations, 2)
	assert.Equal(t, "first", attestations[0].ID)
	assert.Equal(t, "second", attestations[1].ID)
}

func TestClientFetchAttestationsFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		options []Option
		handler func(t *testing.T, bundleBytes []byte) http.Handler
	}{
		{
			name: "missing bundle URL",
			handler: func(t *testing.T, bundleBytes []byte) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprint(w, `{"attestations":[{}]}`)
				})
			},
		},
		{
			name: "bad snappy bundle",
			handler: func(t *testing.T, bundleBytes []byte) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/bundle" {
						fmt.Fprint(w, "not-snappy")
						return
					}
					fmt.Fprintf(w, `{"attestations":[{"bundle_url":%q}]}`, "http://"+r.Host+"/bundle")
				})
			},
		},
		{
			name: "malformed bundle JSON",
			handler: func(t *testing.T, bundleBytes []byte) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/bundle" {
						w.Write(snappy.Encode(nil, []byte("{")))
						return
					}
					fmt.Fprintf(w, `{"attestations":[{"bundle_url":%q}]}`, "http://"+r.Host+"/bundle")
				})
			},
		},
		{
			name:    "pagination exceeds max attestations",
			options: []Option{WithMaxAttestations(1)},
			handler: func(t *testing.T, bundleBytes []byte) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/bundle/one" || r.URL.Path == "/bundle/two" {
						w.Write(bundleBytes)
						return
					}
					fmt.Fprintf(w, `{"attestations":[{"bundle_url":%q},{"bundle_url":%q}]}`, "http://"+r.Host+"/bundle/one", "http://"+r.Host+"/bundle/two")
				})
			},
		},
		{
			name: "API error",
			handler: func(t *testing.T, bundleBytes []byte) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "server failed", http.StatusInternalServerError)
				})
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler(t, compressedBundle(t)))
			t.Cleanup(server.Close)
			client := newTestClient(t, server.URL, tt.options...)
			digest := mustDigest(t, "sha256", repeatHex("ee", 32))

			_, err := client.FetchProvenanceAttestations(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, digest)

			require.Error(t, err)
		})
	}
}

func TestClientWiresIntoCoreVerifier(t *testing.T) {
	bundleBytes := compressedBundle(t)
	assetDigest := mustDigest(t, "sha256", repeatHex("aa", 32))
	releaseDigest := mustDigest(t, "sha1", repeatHex("bb", 20))
	repository := verification.Repository{Owner: "owner", Name: "repo"}
	tag := verification.ReleaseTag("v1.2.3")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/git/ref/tags/v1.2.3":
			fmt.Fprintf(w, `{"object":{"sha":%q}}`, releaseDigest.Hex)
		case "/repos/owner/repo/attestations/" + releaseDigest.String():
			assert.Equal(t, "release", r.URL.Query().Get("predicate_type"))
			fmt.Fprintf(w, `{"attestations":[{"id":"release","bundle_url":%q}]}`, "http://"+r.Host+"/bundle/release")
		case "/repos/owner/repo/attestations/" + assetDigest.String():
			assert.Equal(t, "provenance", r.URL.Query().Get("predicate_type"))
			fmt.Fprintf(w, `{"attestations":[{"id":"provenance","bundle_url":%q}]}`, "http://"+r.Host+"/bundle/provenance")
		case "/bundle/release", "/bundle/provenance":
			w.Write(bundleBytes)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	core, err := verification.NewVerifier(verification.Dependencies{
		ReleaseResolver:   client,
		AttestationSource: client,
		BundleVerifier: fakeBundleVerifier{
			releaseDigest: releaseDigest,
			assetDigest:   assetDigest,
			repository:    repository,
			tag:           tag,
		},
		ArtifactDigester: fakeDigester{digest: assetDigest},
	})
	require.NoError(t, err)

	evidence, err := core.VerifyReleaseAsset(context.Background(), verification.Request{
		Repository: repository,
		Tag:        tag,
		AssetPath:  "/tmp/artifact.tar.gz",
		Policy: verification.Policy{
			TrustedSignerWorkflow: "owner/repo/.github/workflows/release.yml",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, releaseDigest, evidence.ReleaseTagDigest)
	assert.Equal(t, assetDigest, evidence.AssetDigest)
	assert.Equal(t, "release", evidence.ReleaseAttestation.AttestationID)
	assert.Equal(t, "provenance", evidence.ProvenanceAttestation.AttestationID)
}

type fakeBundleVerifier struct {
	releaseDigest verification.Digest
	assetDigest   verification.Digest
	repository    verification.Repository
	tag           verification.ReleaseTag
}

func (f fakeBundleVerifier) Verify(_ context.Context, attestation verification.Attestation, expectedSubject verification.Digest) (verification.VerifiedAttestation, error) {
	requireBundle := func() {
		if _, ok := attestation.Bundle.(*sigbundle.Bundle); !ok {
			panic("adapter did not provide a Sigstore bundle")
		}
	}
	requireBundle()

	timestamps := []verification.VerifiedTimestamp{{Kind: "signed-timestamp", Time: time.Unix(1700000000, 0)}}
	if expectedSubject == f.releaseDigest {
		return verification.VerifiedAttestation{
			Attestation: attestation,
			Certificate: verification.CertificateEvidence{
				SubjectAlternativeName: verification.GitHubReleaseSubjectAlternativeName,
			},
			Statement: verification.Statement{
				PredicateType: verification.ReleasePredicateV01,
				Subjects:      []verification.Subject{{Name: "tag", Digest: f.releaseDigest}, {Name: "artifact", Digest: f.assetDigest}},
				Predicate:     verification.Predicate{ReleaseTag: f.tag},
			},
			VerifiedTimestamps: timestamps,
		}, nil
	}
	return verification.VerifiedAttestation{
		Attestation: attestation,
		Certificate: verification.CertificateEvidence{
			Issuer:                 verification.GitHubActionsOIDCIssuer,
			SubjectAlternativeName: "https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main",
			SourceRepository:       f.repository,
			SignerWorkflow:         "https://github.com/owner/repo/.github/workflows/release.yml@refs/heads/main",
			RunnerEnvironment:      verification.RunnerEnvironmentGitHubHosted,
		},
		Statement: verification.Statement{
			PredicateType: verification.SLSAPredicateV1,
			Subjects:      []verification.Subject{{Name: "artifact", Digest: f.assetDigest}},
		},
		VerifiedTimestamps: timestamps,
	}, nil
}

type fakeDigester struct {
	digest verification.Digest
}

func (f fakeDigester) DigestFile(string) (verification.Digest, error) {
	return f.digest, nil
}

func compressedBundle(t *testing.T) []byte {
	t.Helper()
	bundle := sigdata.Bundle(t, "sigstore.js@2.0.0-provenance.sigstore.json")
	raw, err := bundle.MarshalJSON()
	require.NoError(t, err)
	return snappy.Encode(nil, raw)
}

func newTestClient(t *testing.T, baseURL string, opts ...Option) *Client {
	t.Helper()
	options := append([]Option{WithBaseURL(baseURL)}, opts...)
	client, err := NewClient(options...)
	require.NoError(t, err)
	return client
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
