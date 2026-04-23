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
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

func TestVerifiedInstallerInstallsAfterSuccessfulVerification(t *testing.T) {
	repository := verification.Repository{Owner: "owner", Name: "repo"}
	assetDigest := mustDigest(t, "sha256", repeatHex("aa", 32))
	releaseDigest := mustDigest(t, "sha1", repeatHex("bb", 20))
	tc := newInstallTestContext(t)
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
	tc.files.downloadDir = t.TempDir()
	tc.files.layout = StoreLayout{
		StorePath:    filepath.Join(t.TempDir(), "store"),
		ArtifactPath: filepath.Join(t.TempDir(), "store", "artifact"),
		ExtractedDir: filepath.Join(t.TempDir(), "store", "extracted"),
	}
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/extracted/bin/foo"}}
	tc.files.links = []InstalledBinary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/extracted/bin/foo"}}

	result, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  repository,
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.NoError(t, err)
	assert.Equal(t, "foo-v1.2.3", string(result.Tag))
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", result.AssetName)
	assert.Equal(t, assetDigest, tc.files.storeRequest.AssetDigest)
	assert.Equal(t, tc.downloader.path, tc.files.storeRequest.ArtifactPath)
	assert.Equal(t, tc.files.layout.ArtifactPath, tc.archives.request.ArchivePath)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", tc.archives.request.ArchiveName)
	assert.Equal(t, tc.files.layout.ExtractedDir, tc.archives.request.DestinationDir)
	require.NotNil(t, tc.evidence.record)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", tc.evidence.record.Asset)
	require.NotNil(t, tc.files.metadata)
	assert.Equal(t, assetDigest.String(), tc.files.metadata.AssetDigest)
	assert.Equal(t, tc.evidence.path, tc.files.metadata.VerificationPath)
	require.Len(t, tc.state.saved.Records, 1)
	assert.Equal(t, "owner/repo", tc.state.saved.Records[0].Repository)
	assert.Equal(t, "foo", tc.state.saved.Records[0].Package)
	assert.Equal(t, "1.2.3", tc.state.saved.Records[0].Version)
	assert.Equal(t, assetDigest.String(), tc.state.saved.Records[0].AssetDigest)
	assert.Equal(t, []state.Binary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/extracted/bin/foo"}}, tc.state.saved.Records[0].Binaries)
	assert.Equal(t, []InstalledBinary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/extracted/bin/foo"}}, result.Binaries)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "link", "metadata", "state-add", "cleanup"}, tc.events)
}

func TestVerifiedInstallerDoesNotExtractOrWriteWhenVerificationFails(t *testing.T) {
	tc := newInstallTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.err = errors.New("verification failed")
	tc.files.downloadDir = t.TempDir()

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.False(t, tc.files.storeCalled)
	assert.False(t, tc.archives.called)
	assert.Nil(t, tc.evidence.record)
	assert.Nil(t, tc.files.metadata)
	assert.Equal(t, []string{"state-load", "download-dir", "cleanup"}, tc.events)
}

func TestVerifiedInstallerRejectsDuplicateActiveInstallBeforeDownloading(t *testing.T) {
	tc := newInstallTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)

	_, err = tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")
	assert.False(t, tc.files.storeCalled)
	assert.False(t, tc.archives.called)
	assert.Equal(t, []string{"state-load"}, tc.events)
}

func TestVerifiedInstallerDoesNotWriteMetadataWhenLinkingFails(t *testing.T) {
	tc := newInstallTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
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
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/extracted/bin/foo"}}
	tc.files.linkErr = errors.New("binary link already exists")
	storeDir := filepath.Join(t.TempDir(), "store-root")
	binDir := filepath.Join(t.TempDir(), "bin")

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    storeDir,
		BinDir:      binDir,
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary link already exists")
	assert.NotNil(t, tc.evidence.record)
	assert.Nil(t, tc.files.metadata)
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, storeDir, tc.files.removedManaged.StoreRoot)
	assert.Equal(t, binDir, tc.files.removedManaged.BinRoot)
	assert.Equal(t, tc.files.layout.StorePath, tc.files.removedManaged.StorePath)
	assert.Empty(t, tc.files.removedManaged.Binaries)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "link", "remove-managed", "cleanup"}, tc.events)
}

func TestVerifiedInstallerRollsBackLinksWhenMetadataFails(t *testing.T) {
	tc := newInstallTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
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
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/extracted/bin/foo"}}
	tc.files.links = []InstalledBinary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/extracted/bin/foo"}}
	tc.files.metadataErr = errors.New("disk full")
	storeDir := filepath.Join(t.TempDir(), "store-root")
	binDir := filepath.Join(t.TempDir(), "bin")

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    storeDir,
		BinDir:      binDir,
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write install metadata")
	assert.Nil(t, tc.files.metadata)
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, storeDir, tc.files.removedManaged.StoreRoot)
	assert.Equal(t, binDir, tc.files.removedManaged.BinRoot)
	assert.Equal(t, tc.files.layout.StorePath, tc.files.removedManaged.StorePath)
	assert.Equal(t, tc.files.links, tc.files.removedManaged.Binaries)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "link", "metadata", "remove-managed", "cleanup"}, tc.events)
}

func TestVerifiedInstallerRollsBackLinksWhenInstalledStateFails(t *testing.T) {
	tc := newInstallTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
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
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/extracted/bin/foo"}}
	tc.files.links = []InstalledBinary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/extracted/bin/foo"}}
	tc.state.saveErr = errors.New("disk full")
	storeDir := filepath.Join(t.TempDir(), "store-root")
	binDir := filepath.Join(t.TempDir(), "bin")

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    storeDir,
		BinDir:      binDir,
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "record installed state")
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, storeDir, tc.files.removedManaged.StoreRoot)
	assert.Equal(t, binDir, tc.files.removedManaged.BinRoot)
	assert.Equal(t, tc.files.layout.StorePath, tc.files.removedManaged.StorePath)
	assert.Equal(t, tc.files.links, tc.files.removedManaged.Binaries)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "evidence", "link", "metadata", "state-add", "remove-managed", "cleanup"}, tc.events)
}

func TestVerifiedInstallerRemovesStoreWhenExtractionFails(t *testing.T) {
	tc := newInstallTestContext(t)
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
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
	tc.archives.err = errors.New("malformed archive")
	storeDir := filepath.Join(t.TempDir(), "store-root")
	binDir := filepath.Join(t.TempDir(), "bin")

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    storeDir,
		BinDir:      binDir,
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed archive")
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, storeDir, tc.files.removedManaged.StoreRoot)
	assert.Equal(t, binDir, tc.files.removedManaged.BinRoot)
	assert.Equal(t, tc.files.layout.StorePath, tc.files.removedManaged.StorePath)
	assert.Empty(t, tc.files.removedManaged.Binaries)
	assert.Equal(t, []string{"state-load", "download-dir", "store-layout", "extract", "remove-managed", "cleanup"}, tc.events)
}

type installTestContext struct {
	manifests  *fakeManifestSource
	assets     *fakeReleaseAssetSource
	downloader *fakeArtifactDownloader
	verifier   *fakeVerifier
	evidence   *eventEvidenceWriter
	archives   *fakeArchiveExtractor
	files      *fakeInstallFileSystem
	state      *fakeInstalledStateStore
	events     []string
	subject    *VerifiedInstaller
}

func newInstallTestContext(t *testing.T) *installTestContext {
	t.Helper()
	tc := &installTestContext{
		manifests:  &fakeManifestSource{},
		assets:     &fakeReleaseAssetSource{},
		downloader: &fakeArtifactDownloader{},
		verifier:   &fakeVerifier{},
	}
	tc.evidence = &eventEvidenceWriter{events: &tc.events, path: filepath.Join(t.TempDir(), "verification.json")}
	tc.archives = &fakeArchiveExtractor{events: &tc.events}
	tc.files = &fakeInstallFileSystem{events: &tc.events}
	tc.state = &fakeInstalledStateStore{events: &tc.events, index: state.NewIndex()}
	subject, err := NewVerifiedInstaller(VerifiedInstallDependencies{
		Manifests:      tc.manifests,
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

type eventEvidenceWriter struct {
	events *[]string
	path   string
	record *VerificationRecord
	err    error
}

func (f *eventEvidenceWriter) WriteVerificationEvidence(_ context.Context, _ string, record VerificationRecord) (string, error) {
	*f.events = append(*f.events, "evidence")
	if f.err != nil {
		return "", f.err
	}
	f.record = &record
	return f.path, nil
}

type fakeArchiveExtractor struct {
	events  *[]string
	request ArchiveExtractionRequest
	result  []ExtractedBinary
	err     error
	called  bool
}

func (f *fakeArchiveExtractor) ExtractArchive(_ context.Context, request ArchiveExtractionRequest) ([]ExtractedBinary, error) {
	*f.events = append(*f.events, "extract")
	f.called = true
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

type fakeInstallFileSystem struct {
	events         *[]string
	downloadDir    string
	storeRequest   StoreLayoutRequest
	layout         StoreLayout
	links          []InstalledBinary
	metadata       *InstallRecord
	metadataErr    error
	storeCalled    bool
	linkErr        error
	removedManaged *RemoveManagedInstallRequest
}

func (f *fakeInstallFileSystem) CreateDownloadDir(context.Context) (string, func(), error) {
	*f.events = append(*f.events, "download-dir")
	cleanup := func() {
		*f.events = append(*f.events, "cleanup")
	}
	return f.downloadDir, cleanup, nil
}

func (f *fakeInstallFileSystem) CreateStoreLayout(_ context.Context, request StoreLayoutRequest) (StoreLayout, error) {
	*f.events = append(*f.events, "store-layout")
	f.storeCalled = true
	f.storeRequest = request
	return f.layout, nil
}

func (f *fakeInstallFileSystem) LinkBinaries(_ context.Context, _ LinkBinariesRequest) ([]InstalledBinary, error) {
	*f.events = append(*f.events, "link")
	if f.linkErr != nil {
		return nil, f.linkErr
	}
	return f.links, nil
}

func (f *fakeInstallFileSystem) RemoveManagedInstall(_ context.Context, request RemoveManagedInstallRequest) error {
	*f.events = append(*f.events, "remove-managed")
	copied := request
	copied.Binaries = append([]InstalledBinary(nil), request.Binaries...)
	f.removedManaged = &copied
	return nil
}

func (f *fakeInstallFileSystem) WriteInstallMetadata(_ context.Context, _ string, record InstallRecord) (string, error) {
	*f.events = append(*f.events, "metadata")
	if f.metadataErr != nil {
		return "", f.metadataErr
	}
	f.metadata = &record
	return filepath.Join(record.StorePath, "install.json"), nil
}

type fakeInstalledStateStore struct {
	events  *[]string
	index   state.Index
	saved   state.Index
	loadErr error
	saveErr error
}

func (f *fakeInstalledStateStore) LoadInstalledState(context.Context, string) (state.Index, error) {
	if f.events != nil {
		*f.events = append(*f.events, "state-load")
	}
	if f.loadErr != nil {
		return state.Index{}, f.loadErr
	}
	return f.index.Normalize(), nil
}

func (f *fakeInstalledStateStore) AddInstalledRecord(_ context.Context, _ string, record state.Record) (state.Index, error) {
	if f.events != nil {
		*f.events = append(*f.events, "state-add")
	}
	if f.saveErr != nil {
		return state.Index{}, f.saveErr
	}
	index, err := f.index.AddRecord(record)
	if err != nil {
		return state.Index{}, err
	}
	f.saved = index.Normalize()
	f.index = f.saved
	return f.saved, nil
}
