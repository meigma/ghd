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
	tc.downloader.path = filepath.Join(tc.files.downloadDir, "foo_1.2.3_darwin_arm64.tar.gz")
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
	outputDir := t.TempDir()

	result, err := tc.subject.Download(context.Background(), VerifiedDownloadRequest{
		Repository:  repository,
		PackageName: "foo",
		Version:     "1.2.3",
		OutputDir:   outputDir,
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.NoError(t, err)
	assert.Equal(t, "foo-v1.2.3", string(result.Tag))
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", result.AssetName)
	assert.Equal(t, filepath.Join(outputDir, "foo_1.2.3_darwin_arm64.tar.gz"), result.ArtifactPath)
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
	assert.Equal(t, tc.files.downloadDir, tc.downloader.request.OutputDir)
	assert.Equal(t, tc.downloader.path, tc.verifier.request.AssetPath)
	assert.Equal(t, tc.downloader.path, tc.files.publishSourcePath)
	assert.Equal(t, outputDir, tc.files.publishOutputDir)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", tc.files.publishAssetName)
	assert.True(t, tc.files.cleaned)
	assert.Equal(t, []string{"download-dir", "download", "verify", "publish", "evidence"}, tc.events)
	assert.Nil(t, tc.downloader.request.Progress)
}

func TestVerifiedDownloaderReportsProgressInOrder(t *testing.T) {
	repository := verification.Repository{Owner: "owner", Name: "repo"}
	tc := newDownloadTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.downloader.progress = []DownloadProgress{
		{AssetName: "foo_1.2.3_darwin_arm64.tar.gz", BytesDownloaded: 128, TotalBytes: 512},
		{AssetName: "foo_1.2.3_darwin_arm64.tar.gz", BytesDownloaded: 512, TotalBytes: 512},
	}
	tc.verifier.evidence = verification.Evidence{
		Repository:       repository,
		Tag:              "foo-v1.2.3",
		AssetDigest:      mustDigest(t, "sha256", repeatHex("aa", 32)),
		ReleaseTagDigest: mustDigest(t, "sha1", repeatHex("bb", 20)),
	}

	var stages []VerifiedDownloadProgressStage
	var downloads []DownloadProgress
	_, err := tc.subject.Download(context.Background(), VerifiedDownloadRequest{
		Repository:  repository,
		PackageName: "foo",
		Version:     "1.2.3",
		OutputDir:   t.TempDir(),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
		Progress: func(progress VerifiedDownloadProgress) {
			stages = append(stages, progress.Stage)
			if progress.Download != nil {
				downloads = append(downloads, *progress.Download)
			}
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []VerifiedDownloadProgressStage{
		VerifiedDownloadProgressResolvingManifest,
		VerifiedDownloadProgressResolvingAsset,
		VerifiedDownloadProgressDownloading,
		VerifiedDownloadProgressDownloading,
		VerifiedDownloadProgressVerifying,
		VerifiedDownloadProgressWritingEvidence,
	}, stages)
	assert.Equal(t, tc.downloader.progress, downloads)
	require.NotNil(t, tc.downloader.request.Progress)
}

func TestVerifiedDownloaderDoesNotWriteEvidenceWhenVerificationFails(t *testing.T) {
	tc := newDownloadTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(tc.files.downloadDir, "foo.tar.gz")
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
	assert.Empty(t, tc.files.publishSourcePath)
	assert.True(t, tc.files.cleaned)
	assert.Equal(t, []string{"download-dir", "download", "verify"}, tc.events)
}

func TestVerifiedDownloaderDoesNotWriteEvidenceWhenPublishFails(t *testing.T) {
	tc := newDownloadTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(tc.files.downloadDir, "foo.tar.gz")
	tc.verifier.evidence = verification.Evidence{
		Repository:       verification.Repository{Owner: "owner", Name: "repo"},
		Tag:              "foo-v1.2.3",
		AssetDigest:      mustDigest(t, "sha256", repeatHex("aa", 32)),
		ReleaseTagDigest: mustDigest(t, "sha1", repeatHex("bb", 20)),
	}
	tc.files.publishErr = errors.New("output artifact exists")

	_, err := tc.subject.Download(context.Background(), VerifiedDownloadRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		OutputDir:   t.TempDir(),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish verified artifact")
	assert.Nil(t, tc.writer.record)
	assert.Equal(t, tc.downloader.path, tc.files.publishSourcePath)
	assert.True(t, tc.files.cleaned)
	assert.Equal(t, []string{"download-dir", "download", "verify", "publish"}, tc.events)
}

func TestVerifiedDownloaderUsesReleaseManifestForTrustPolicy(t *testing.T) {
	tc := newDownloadTestContext(t)
	tc.manifests.data = []byte(maliciousDiscoveryManifest())
	tc.manifests.refData = map[string][]byte{
		"foo-v1.2.3": []byte(testManifest()),
	}
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(tc.files.downloadDir, "foo.tar.gz")
	tc.verifier.evidence = verification.Evidence{
		Repository:       verification.Repository{Owner: "owner", Name: "repo"},
		Tag:              "foo-v1.2.3",
		AssetDigest:      mustDigest(t, "sha256", repeatHex("aa", 32)),
		ReleaseTagDigest: mustDigest(t, "sha1", repeatHex("bb", 20)),
	}

	_, err := tc.subject.Download(context.Background(), VerifiedDownloadRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		OutputDir:   t.TempDir(),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.NoError(t, err)
	assert.Equal(t, verification.ReleaseTag("foo-v1.2.3"), tc.assets.tag)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", tc.assets.assetName)
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"), tc.verifier.request.Policy.TrustedSignerWorkflow)
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
	files      *fakeDownloadFileSystem
	subject    *VerifiedDownloader
	events     []string
}

func newDownloadTestContext(t *testing.T) *downloadTestContext {
	t.Helper()
	tc := &downloadTestContext{
		manifests:  &fakeManifestSource{},
		assets:     &fakeReleaseAssetSource{},
		downloader: &fakeArtifactDownloader{},
		verifier:   &fakeVerifier{},
		writer:     &fakeEvidenceWriter{},
		files:      &fakeDownloadFileSystem{downloadDir: t.TempDir()},
	}
	tc.downloader.events = &tc.events
	tc.verifier.events = &tc.events
	tc.writer.events = &tc.events
	tc.files.events = &tc.events
	subject, err := NewVerifiedDownloader(VerifiedDownloadDependencies{
		Manifests:      tc.manifests,
		Assets:         tc.assets,
		Downloader:     tc.downloader,
		Verifier:       tc.verifier,
		EvidenceWriter: tc.writer,
		FileSystem:     tc.files,
	})
	require.NoError(t, err)
	tc.subject = subject
	return tc
}

type fakeManifestSource struct {
	data    []byte
	refData map[string][]byte
	err     error
	refErr  map[string]error
}

func (f *fakeManifestSource) FetchManifest(context.Context, verification.Repository) ([]byte, error) {
	return f.data, f.err
}

func (f *fakeManifestSource) FetchManifestAtRef(_ context.Context, _ verification.Repository, ref string) ([]byte, error) {
	if err, ok := f.refErr[ref]; ok {
		return nil, err
	}
	if data, ok := f.refData[ref]; ok {
		return data, nil
	}
	return f.data, f.err
}

type fakeReleaseAssetSource struct {
	asset     ReleaseAsset
	tag       verification.ReleaseTag
	assetName string
	err       error
}

func (f *fakeReleaseAssetSource) ResolveReleaseAsset(_ context.Context, _ verification.Repository, tag verification.ReleaseTag, assetName string) (ReleaseAsset, error) {
	f.tag = tag
	f.assetName = assetName
	return f.asset, f.err
}

type fakeArtifactDownloader struct {
	path     string
	err      error
	request  DownloadReleaseAssetRequest
	progress []DownloadProgress
	events   *[]string
}

func (f *fakeArtifactDownloader) DownloadReleaseAsset(_ context.Context, request DownloadReleaseAssetRequest) (string, error) {
	f.request = request
	if f.events != nil {
		*f.events = append(*f.events, "download")
	}
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
	events *[]string
}

func (f *fakeEvidenceWriter) WriteVerificationEvidence(_ context.Context, outputDir string, record VerificationRecord) (string, error) {
	if f.events != nil {
		*f.events = append(*f.events, "evidence")
	}
	if f.err != nil {
		return "", f.err
	}
	f.record = &record
	return filepath.Join(outputDir, "verification.json"), nil
}

type fakeDownloadFileSystem struct {
	downloadDir       string
	downloadErr       error
	cleaned           bool
	publishSourcePath string
	publishOutputDir  string
	publishAssetName  string
	publishErr        error
	events            *[]string
}

func (f *fakeDownloadFileSystem) CreateDownloadDir(context.Context) (string, func(), error) {
	if f.events != nil {
		*f.events = append(*f.events, "download-dir")
	}
	if f.downloadErr != nil {
		return "", nil, f.downloadErr
	}
	return f.downloadDir, func() {
		f.cleaned = true
	}, nil
}

func (f *fakeDownloadFileSystem) PublishVerifiedArtifact(_ context.Context, sourcePath string, outputDir string, assetName string) (string, error) {
	if f.events != nil {
		*f.events = append(*f.events, "publish")
	}
	f.publishSourcePath = sourcePath
	f.publishOutputDir = outputDir
	f.publishAssetName = assetName
	if f.publishErr != nil {
		return "", f.publishErr
	}
	return filepath.Join(outputDir, assetName), nil
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

func maliciousDiscoveryManifest() string {
	return `
version = 1

[provenance]
signer_workflow = "owner/repo/.github/workflows/evil.yml"

[[packages]]
name = "foo"
tag_pattern = "foo-v${version}"

[[packages.assets]]
os = "darwin"
arch = "arm64"
pattern = "evil_${version}_darwin_arm64.tar.gz"

[[packages.binaries]]
path = "bin/evil"
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
