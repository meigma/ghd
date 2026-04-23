package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

func TestPackageUpdaterUpdatesInstalledPackageAfterSuccessfulStaging(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.2.3", AssetNames: []string{"foo_1.2.3_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
	}
	tc.assets.asset = ReleaseAsset{Name: "foo_1.3.0_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.evidence = verification.Evidence{
		AssetDigest: mustDigest(t, "sha256", repeatHex("aa", 32)),
	}
	tc.files.downloadDir = t.TempDir()
	tc.files.layout = StoreLayout{
		StorePath:    filepath.Join(t.TempDir(), "store"),
		ArtifactPath: filepath.Join(t.TempDir(), "store", "artifact"),
		ExtractedDir: filepath.Join(t.TempDir(), "store", "extracted"),
	}
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/new/extracted/bin/foo"}}
	storeDir := filepath.Join(t.TempDir(), "store-root")
	binDir := filepath.Join(t.TempDir(), "bin")

	result, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: storeDir,
		BinDir:   binDir,
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	require.True(t, result.Updated)
	assert.Equal(t, "1.2.3", result.Previous.Version)
	assert.Equal(t, "1.3.0", result.Current.Version)
	require.NotNil(t, tc.files.metadata)
	assert.Equal(t, "1.3.0", tc.files.metadata.Version)
	assert.Equal(t, "foo_1.3.0_darwin_arm64.tar.gz", tc.files.metadata.Asset)
	require.Len(t, tc.files.replaceRequests, 1)
	assert.Equal(t, "/bin/foo", tc.files.replaceRequests[0].Previous[0].LinkPath)
	expectedLinkPath, err := filepath.Abs(filepath.Join(binDir, "foo"))
	require.NoError(t, err)
	assert.Equal(t, expectedLinkPath, tc.files.replaceRequests[0].Next[0].LinkPath)
	assert.Equal(t, "1.3.0", tc.state.replacedRecord.Version)
	assert.Equal(t, tc.files.layout.StorePath, tc.state.replacedRecord.StorePath)
	assert.Equal(t, storeDir, tc.files.removedStoreRoot)
	assert.Equal(t, "/store/foo", tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "remove-store", "cleanup"}, tc.events)
}

func TestPackageUpdaterReturnsNoOpWhenNoNewerStableReleaseExists(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	record := installedRecord("owner/repo", "foo")
	record.Version = "1.3.0"
	record.Tag = "foo-v1.3.0"
	record.Asset = "foo_1.3.0_darwin_arm64.tar.gz"
	tc.state.index, err = tc.state.index.AddRecord(record)
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.3.0-rc.1", Prerelease: true, AssetNames: []string{"foo_1.3.0-rc.1_darwin_arm64.tar.gz"}},
	}

	result, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	assert.False(t, result.Updated)
	assert.Equal(t, record.Version, result.Current.Version)
	assert.Empty(t, tc.files.replaceRequests)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load"}, tc.events)
}

func TestPackageUpdaterFailsSafelyWhenInstalledAssetVariantDriftsFromManifest(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(`
version = 1

[provenance]
signer_workflow = "owner/repo/.github/workflows/release.yml"

[[packages]]
name = "foo"
tag_pattern = "foo-v${version}"

[[packages.assets]]
os = "linux"
arch = "amd64"
pattern = "foo_${version}_linux_amd64.tar.gz"

[[packages.binaries]]
path = "bin/foo"
`)
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_linux_amd64.tar.gz"}},
	}

	_, err = tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "installed asset")
	assert.Empty(t, tc.files.replaceRequests)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load"}, tc.events)
}

func TestPackageUpdaterRollsBackActiveLinksWhenStateReplacementFails(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.state.replaceErr = errors.New("write installed state")
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
	}
	tc.assets.asset = ReleaseAsset{Name: "foo_1.3.0_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.evidence = verification.Evidence{
		AssetDigest: mustDigest(t, "sha256", repeatHex("aa", 32)),
	}
	tc.files.downloadDir = t.TempDir()
	tc.files.layout = StoreLayout{
		StorePath:    filepath.Join(t.TempDir(), "store"),
		ArtifactPath: filepath.Join(t.TempDir(), "store", "artifact"),
		ExtractedDir: filepath.Join(t.TempDir(), "store", "extracted"),
	}
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/new/extracted/bin/foo"}}

	_, err = tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace installed state")
	require.Len(t, tc.files.replaceRequests, 2)
	assert.Equal(t, "/bin/foo", tc.files.replaceRequests[0].Previous[0].LinkPath)
	assert.Equal(t, "/bin/foo", tc.files.replaceRequests[1].Next[0].LinkPath)
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, tc.files.layout.StorePath, tc.files.removedManaged.StorePath)
	assert.Empty(t, tc.files.removedManaged.Binaries)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "replace-binaries", "remove-managed", "cleanup"}, tc.events)
}

func TestPackageUpdaterPreservesStagedStoreWhenRollbackFails(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.state.replaceErr = errors.New("write installed state")
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
	}
	tc.assets.asset = ReleaseAsset{Name: "foo_1.3.0_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.evidence = verification.Evidence{
		AssetDigest: mustDigest(t, "sha256", repeatHex("aa", 32)),
	}
	tc.files.downloadDir = t.TempDir()
	tc.files.layout = StoreLayout{
		StorePath:    filepath.Join(t.TempDir(), "store"),
		ArtifactPath: filepath.Join(t.TempDir(), "store", "artifact"),
		ExtractedDir: filepath.Join(t.TempDir(), "store", "extracted"),
	}
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/new/extracted/bin/foo"}}
	tc.files.replaceErrs = []error{nil, errors.New("restore links")}

	_, err = tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace installed state")
	assert.Contains(t, err.Error(), "restore previous managed binaries")
	assert.Contains(t, err.Error(), "preserved staged update")
	require.Len(t, tc.files.replaceRequests, 2)
	assert.Nil(t, tc.files.removedManaged)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "replace-binaries", "cleanup"}, tc.events)
}

func TestPackageUpdaterReturnsUpdatedResultWhenPreviousStoreCleanupFails(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
	}
	tc.assets.asset = ReleaseAsset{Name: "foo_1.3.0_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.evidence = verification.Evidence{
		AssetDigest: mustDigest(t, "sha256", repeatHex("aa", 32)),
	}
	tc.files.downloadDir = t.TempDir()
	tc.files.layout = StoreLayout{
		StorePath:    filepath.Join(t.TempDir(), "store"),
		ArtifactPath: filepath.Join(t.TempDir(), "store", "artifact"),
		ExtractedDir: filepath.Join(t.TempDir(), "store", "extracted"),
	}
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/new/extracted/bin/foo"}}
	tc.files.removeStoreErr = errors.New("permission denied")

	result, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.True(t, result.Updated)
	assert.Equal(t, "1.3.0", result.Current.Version)
	assert.Contains(t, err.Error(), "updated owner/repo/foo@1.2.3 -> 1.3.0 but failed to remove previous store")
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "remove-store", "cleanup"}, tc.events)
}

type packageUpdaterTestContext struct {
	manifests  *fakeManifestRouter
	releases   *fakeRepositoryReleaseSource
	assets     *fakeReleaseAssetSource
	downloader *fakeArtifactDownloader
	verifier   *fakeVerifier
	evidence   *eventEvidenceWriter
	archives   *fakeArchiveExtractor
	files      *fakeInstallFileSystem
	state      *fakeInstalledStateStore
	events     []string
	subject    *PackageUpdater
}

func newPackageUpdaterTestContext(t *testing.T) *packageUpdaterTestContext {
	t.Helper()
	tc := &packageUpdaterTestContext{
		manifests: &fakeManifestRouter{
			data: map[string][]byte{},
			err:  map[string]error{},
		},
		releases: &fakeRepositoryReleaseSource{
			data: map[string][]RepositoryRelease{},
			err:  map[string]error{},
		},
		assets:     &fakeReleaseAssetSource{},
		downloader: &fakeArtifactDownloader{},
		verifier:   &fakeVerifier{},
	}
	tc.evidence = &eventEvidenceWriter{events: &tc.events, path: filepath.Join(t.TempDir(), "verification.json")}
	tc.archives = &fakeArchiveExtractor{events: &tc.events}
	tc.files = &fakeInstallFileSystem{events: &tc.events}
	tc.state = &fakeInstalledStateStore{events: &tc.events, index: state.NewIndex()}
	subject, err := NewPackageUpdater(PackageUpdaterDependencies{
		Manifests:      tc.manifests,
		Releases:       tc.releases,
		Assets:         tc.assets,
		Downloader:     tc.downloader,
		Verifier:       tc.verifier,
		EvidenceWriter: tc.evidence,
		Archives:       tc.archives,
		FileSystem:     tc.files,
		StateStore:     tc.state,
		Now:            func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	require.NoError(t, err)
	tc.subject = subject
	return tc
}
