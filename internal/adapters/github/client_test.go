package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/snappy"
	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	sigdata "github.com/sigstore/sigstore-go/pkg/testing/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/app"
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

func TestClientFetchManifestUsesRawContentsAPI(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		assert.Equal(t, "/repos/owner/repo/contents/ghd.toml", r.URL.Path)
		fmt.Fprint(w, "version = 1\n")
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithToken("token-123"))

	data, err := client.FetchManifest(context.Background(), verification.Repository{Owner: "owner", Name: "repo"})

	require.NoError(t, err)
	assert.Equal(t, "version = 1\n", string(data))
	assert.Equal(t, "application/vnd.github.raw", gotHeader.Get("Accept"))
	assert.Equal(t, "Bearer token-123", gotHeader.Get("Authorization"))
}

func TestClientFetchManifestAcceptsContentsJSON(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("version = 1\n"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"encoding":"base64","content":%q}`, encoded)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	data, err := client.FetchManifest(context.Background(), verification.Repository{Owner: "owner", Name: "repo"})

	require.NoError(t, err)
	assert.Equal(t, "version = 1\n", string(data))
}

func TestClientResolveReleaseAssetSelectsExactName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/owner/repo/releases/tags/v1.2.3", r.URL.Path)
		fmt.Fprintf(w, `{"assets":[{"name":"other.tar.gz","browser_download_url":"http://%s/other"},{"name":"foo.tar.gz","browser_download_url":"http://%s/foo"}]}`, r.Host, r.Host)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)

	asset, err := client.ResolveReleaseAsset(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, "v1.2.3", "foo.tar.gz")

	require.NoError(t, err)
	assert.Equal(t, "foo.tar.gz", asset.Name)
	assert.Equal(t, "http://"+server.Listener.Addr().String()+"/foo", asset.DownloadURL)
}

func TestClientListRepositoryReleasesFollowsPagination(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		assert.Equal(t, "/repos/owner/repo/releases", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `[{"tag_name":"foo-v1.3.0","prerelease":true,"assets":[{"name":"foo_1.3.0_darwin_arm64.tar.gz"}]}]`)
			return
		}
		next := "http://" + r.Host + r.URL.Path + "?" + r.URL.RawQuery + "&page=2"
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, next))
		fmt.Fprint(w, `[{"tag_name":"foo-v1.2.3","assets":[{"name":"foo_1.2.3_darwin_arm64.tar.gz"}]},{"tag_name":"foo-v1.2.4","draft":true,"assets":[{"name":"foo_1.2.4_darwin_arm64.tar.gz"}]}]`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithToken("token-123"))

	releases, err := client.ListRepositoryReleases(context.Background(), verification.Repository{Owner: "owner", Name: "repo"})

	require.NoError(t, err)
	require.Len(t, releases, 3)
	assert.Equal(t, "foo-v1.2.3", releases[0].TagName)
	assert.Equal(t, []string{"foo_1.2.3_darwin_arm64.tar.gz"}, releases[0].AssetNames)
	assert.True(t, releases[1].Draft)
	assert.True(t, releases[2].Prerelease)
	assert.Equal(t, "Bearer token-123", gotHeader.Get("Authorization"))
}

func TestClientCheckRateLimit(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		assert.Equal(t, "/rate_limit", r.URL.Path)
		fmt.Fprint(w, `{"resources":{"core":{"limit":5000,"remaining":4999,"used":1}}}`)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithToken("token-123"))

	status, err := client.CheckRateLimit(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 5000, status.CoreLimit)
	assert.Equal(t, 4999, status.CoreRemaining)
	assert.Equal(t, 1, status.CoreUsed)
	assert.Equal(t, "Bearer token-123", gotHeader.Get("Authorization"))
}

func TestClientDownloadReleaseAssetDoesNotSendGitHubTokenToAssetURL(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		assert.Equal(t, "/asset/foo.tar.gz", r.URL.Path)
		fmt.Fprint(w, "artifact bytes")
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL, WithToken("token-123"), WithUserAgent("ghd-test"))
	outputDir := t.TempDir()

	path, err := client.DownloadReleaseAsset(context.Background(), app.DownloadReleaseAssetRequest{
		Asset: app.ReleaseAsset{
			Name:        "foo.tar.gz",
			DownloadURL: server.URL + "/asset/foo.tar.gz",
		},
		OutputDir: outputDir,
	})

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(outputDir, "foo.tar.gz"), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "artifact bytes", string(data))
	assert.Empty(t, gotHeader.Get("Authorization"), "asset URL requests must not receive the GitHub token")
	assert.Equal(t, "ghd-test", gotHeader.Get("User-Agent"))
}

func TestClientDownloadReleaseAssetReportsProgress(t *testing.T) {
	payload := make([]byte, 70*1024)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	client := newTestClient(t, server.URL)
	outputDir := t.TempDir()
	var progress []app.DownloadProgress
	path, err := client.DownloadReleaseAsset(context.Background(), app.DownloadReleaseAssetRequest{
		Asset: app.ReleaseAsset{
			Name:        "foo.tar.gz",
			DownloadURL: server.URL + "/asset/foo.tar.gz",
		},
		OutputDir: outputDir,
		Progress: func(got app.DownloadProgress) {
			progress = append(progress, got)
		},
	})

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(outputDir, "foo.tar.gz"), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, payload, data)
	require.NotEmpty(t, progress)
	assert.Equal(t, app.DownloadProgress{
		AssetName:       "foo.tar.gz",
		BytesDownloaded: 0,
		TotalBytes:      int64(len(payload)),
	}, progress[0])
	last := progress[len(progress)-1]
	assert.Equal(t, "foo.tar.gz", last.AssetName)
	assert.Equal(t, int64(len(payload)), last.BytesDownloaded)
	assert.Equal(t, int64(len(payload)), last.TotalBytes)
	var sawIntermediate bool
	for _, event := range progress {
		if event.BytesDownloaded > 0 && event.BytesDownloaded < int64(len(payload)) {
			sawIntermediate = true
		}
	}
	assert.True(t, sawIntermediate, "expected at least one progress event before completion")
}

func TestClientReleaseAssetOperationsFailClosed(t *testing.T) {
	t.Run("release asset API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "missing", http.StatusNotFound)
		}))
		t.Cleanup(server.Close)
		client := newTestClient(t, server.URL)

		_, err := client.ResolveReleaseAsset(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, "v1.2.3", "foo.tar.gz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 404")
	})

	t.Run("malformed release response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "{")
		}))
		t.Cleanup(server.Close)
		client := newTestClient(t, server.URL)

		_, err := client.ResolveReleaseAsset(context.Background(), verification.Repository{Owner: "owner", Name: "repo"}, "v1.2.3", "foo.tar.gz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode GitHub response")
	})

	t.Run("list releases malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "{")
		}))
		t.Cleanup(server.Close)
		client := newTestClient(t, server.URL)

		_, err := client.ListRepositoryReleases(context.Background(), verification.Repository{Owner: "owner", Name: "repo"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode GitHub response")
	})

	t.Run("download rejects unsafe asset name", func(t *testing.T) {
		client := newTestClient(t, "https://api.github.test")

		_, err := client.DownloadReleaseAsset(context.Background(), app.DownloadReleaseAssetRequest{
			Asset: app.ReleaseAsset{
				Name:        "../foo.tar.gz",
				DownloadURL: "https://example.test/foo.tar.gz",
			},
			OutputDir: t.TempDir(),
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "path separators")
	})
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
