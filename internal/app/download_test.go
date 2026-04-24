package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

func TestVerifiedDownloaderWritesEvidenceAfterSuccessfulVerification(t *testing.T) {
	repository := verification.Repository{Owner: "owner", Name: "repo"}
	assetDigest := mustDigest(t, "sha256", repeatHex("aa", 32))
	releaseDigest := mustDigest(t, "sha1", repeatHex("bb", 20))
	tc := newDownloadTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.evidence = verification.Evidence{
		Repository:       repository,
		Tag:              "foo-v1.2.3",
		AssetDigest:      assetDigest,
		ReleaseTagDigest: releaseDigest,
		ReleaseAttestation: verification.AttestationEvidence{
			AttestationID: "release",
		},
		ProvenanceAttestation: verification.AttestationEvidence{
			AttestationID: "provenance",
		},
	}

	result, err := tc.subject.Download(context.Background(), VerifiedDownloadRequest{
		Repository:  repository,
		PackageName: "foo",
		Version:     "1.2.3",
		OutputDir:   t.TempDir(),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.NoError(t, err)
	assert.Equal(t, "foo-v1.2.3", string(result.Tag))
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", result.AssetName)
	assert.Equal(t, tc.downloader.path, result.ArtifactPath)
	assert.Equal(t, "verification.json", filepath.Base(result.EvidencePath))
	require.NotNil(t, tc.writer.record)
	assert.Equal(t, "owner/repo", tc.writer.record.Repository)
	assert.Equal(t, "foo", tc.writer.record.Package)
	assert.Equal(t, "1.2.3", tc.writer.record.Version)
	assert.Equal(t, "foo-v1.2.3", tc.writer.record.Tag)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", tc.writer.record.Asset)
	assert.Equal(t, assetDigest, tc.writer.record.Evidence.AssetDigest)
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"), tc.verifier.request.Policy.TrustedSignerWorkflow)
	assert.True(t, tc.verifier.request.Policy.ExpectedSourceRepository.IsZero(), "core verifier should apply the repository default")
	assert.Nil(t, tc.downloader.request.Progress)
}

func TestVerifiedDownloaderDoesNotWriteEvidenceWhenVerificationFails(t *testing.T) {
	tc := newDownloadTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.err = errors.New("verification failed")

	_, err := tc.subject.Download(context.Background(), VerifiedDownloadRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		OutputDir:   t.TempDir(),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.Nil(t, tc.writer.record)
}

func TestVerifiedDownloaderReportsMissingPackageAndAsset(t *testing.T) {
	tests := []struct {
		name    string
		request VerifiedDownloadRequest
		want    string
	}{
		{
			name: "missing package",
			request: VerifiedDownloadRequest{
				Repository:  verification.Repository{Owner: "owner", Name: "repo"},
				PackageName: "missing",
				Version:     "1.2.3",
				OutputDir:   t.TempDir(),
				Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
			},
			want: `package "missing"`,
		},
		{
			name: "missing platform asset",
			request: VerifiedDownloadRequest{
				Repository:  verification.Repository{Owner: "owner", Name: "repo"},
				PackageName: "foo",
				Version:     "1.2.3",
				OutputDir:   t.TempDir(),
				Platform:    manifest.Platform{OS: "linux", Arch: "arm64"},
			},
			want: "no asset",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tc := newDownloadTestContext(t)
			tc.manifests.data = []byte(testManifest())

			_, err := tc.subject.Download(context.Background(), tt.request)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

type downloadTestContext struct {
	manifests  *fakeManifestSource
	assets     *fakeReleaseAssetSource
	downloader *fakeArtifactDownloader
	verifier   *fakeVerifier
	writer     *fakeEvidenceWriter
	subject    *VerifiedDownloader
}

func newDownloadTestContext(t *testing.T) *downloadTestContext {
	t.Helper()
	tc := &downloadTestContext{
		manifests:  &fakeManifestSource{},
		assets:     &fakeReleaseAssetSource{},
		downloader: &fakeArtifactDownloader{},
		verifier:   &fakeVerifier{},
		writer:     &fakeEvidenceWriter{},
	}
	subject, err := NewVerifiedDownloader(VerifiedDownloadDependencies{
		Manifests:      tc.manifests,
		Assets:         tc.assets,
		Downloader:     tc.downloader,
		Verifier:       tc.verifier,
		EvidenceWriter: tc.writer,
	})
	require.NoError(t, err)
	tc.subject = subject
	return tc
}

type fakeManifestSource struct {
	data []byte
	err  error
}

func (f *fakeManifestSource) FetchManifest(context.Context, verification.Repository) ([]byte, error) {
	return f.data, f.err
}

type fakeReleaseAssetSource struct {
	asset ReleaseAsset
	err   error
}

func (f *fakeReleaseAssetSource) ResolveReleaseAsset(context.Context, verification.Repository, verification.ReleaseTag, string) (ReleaseAsset, error) {
	return f.asset, f.err
}

type fakeArtifactDownloader struct {
	path     string
	err      error
	request  DownloadReleaseAssetRequest
	progress []DownloadProgress
}

func (f *fakeArtifactDownloader) DownloadReleaseAsset(_ context.Context, request DownloadReleaseAssetRequest) (string, error) {
	f.request = request
	for _, progress := range f.progress {
		if request.Progress != nil {
			request.Progress(progress)
		}
	}
	return f.path, f.err
}

type fakeVerifier struct {
	request  verification.Request
	evidence verification.Evidence
	err      error
	events   *[]string
}

func (f *fakeVerifier) VerifyReleaseAsset(_ context.Context, request verification.Request) (verification.Evidence, error) {
	f.request = request
	if f.events != nil {
		*f.events = append(*f.events, "verify")
	}
	if f.err != nil {
		return verification.Evidence{}, f.err
	}
	if f.evidence.ReleaseAttestation.VerifiedTimestamps == nil {
		f.evidence.ReleaseAttestation.VerifiedTimestamps = []verification.VerifiedTimestamp{{Kind: "test", Time: time.Unix(1700000000, 0)}}
	}
	return f.evidence, nil
}

type fakeEvidenceWriter struct {
	record *VerificationRecord
	err    error
}

func (f *fakeEvidenceWriter) WriteVerificationEvidence(_ context.Context, outputDir string, record VerificationRecord) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.record = &record
	return filepath.Join(outputDir, "verification.json"), nil
}

func testManifest() string {
	return `
version = 1

[provenance]
signer_workflow = "owner/repo/.github/workflows/release.yml"

[[packages]]
name = "foo"
tag_pattern = "foo-v${version}"

[[packages.assets]]
os = "darwin"
arch = "arm64"
pattern = "foo_${version}_darwin_arm64.tar.gz"

[[packages.binaries]]
path = "bin/foo"
`
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
