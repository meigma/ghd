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
	assert.Nil(t, tc.downloader.request.Progress)
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

func TestVerifiedInstallerReportsProgressInInstallOrder(t *testing.T) {
	tc := newInstallTestContext(t)
	givenSuccessfulInstallFixture(t, tc)
	var stages []InstallProgressStage

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
		Progress: func(progress InstallProgress) {
			stages = append(stages, progress.Stage)
		},
	})

	require.NoError(t, err)
	assert.Equal(t, []InstallProgressStage{
		InstallProgressCheckingState,
		InstallProgressFetchingManifest,
		InstallProgressResolvingPackage,
		InstallProgressResolvingAsset,
		InstallProgressPreparingDownload,
		InstallProgressDownloading,
		InstallProgressVerifying,
		InstallProgressAwaitingApproval,
		InstallProgressPreparingStore,
		InstallProgressExtracting,
		InstallProgressWritingEvidence,
		InstallProgressLinkingBinaries,
		InstallProgressWritingMetadata,
		InstallProgressRecordingState,
	}, stages)
}

func TestVerifiedInstallerForwardsDownloadProgressBeforeVerification(t *testing.T) {
	tc := newInstallTestContext(t)
	givenSuccessfulInstallFixture(t, tc)
	tc.verifier.events = &tc.events
	tc.downloader.progress = []DownloadProgress{
		{AssetName: "foo_1.2.3_darwin_arm64.tar.gz", BytesDownloaded: 0, TotalBytes: 100},
		{AssetName: "foo_1.2.3_darwin_arm64.tar.gz", BytesDownloaded: 40, TotalBytes: 100},
		{AssetName: "foo_1.2.3_darwin_arm64.tar.gz", BytesDownloaded: 100, TotalBytes: 100},
	}
	var downloads []DownloadProgress

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
		Progress: func(progress InstallProgress) {
			if progress.Download == nil {
				return
			}
			downloads = append(downloads, *progress.Download)
			tc.events = append(tc.events, "download-progress")
		},
	})

	require.NoError(t, err)
	assert.Equal(t, tc.downloader.progress, downloads)
	assert.Equal(t, []string{
		"state-load",
		"download-dir",
		"download-progress",
		"download-progress",
		"download-progress",
		"verify",
		"store-layout",
		"extract",
		"evidence",
		"link",
		"metadata",
		"state-add",
		"cleanup",
	}, tc.events)
}

func TestVerifiedInstallerApprovalReceivesVerifiedFacts(t *testing.T) {
	tc := newInstallTestContext(t)
	evidence := givenSuccessfulInstallFixture(t, tc)
	var approval InstallApproval

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      "/managed/bin",
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
		Approve: func(_ context.Context, got InstallApproval) error {
			approval = got
			return nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, approval.Repository)
	assert.Equal(t, "foo", approval.PackageName.String())
	assert.Equal(t, "1.2.3", approval.Version.String())
	assert.Equal(t, verification.ReleaseTag("foo-v1.2.3"), approval.Tag)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", approval.AssetName)
	assert.Equal(t, evidence.AssetDigest, approval.AssetDigest)
	assert.Equal(t, verification.ReleasePredicateV02, approval.ReleasePredicateType)
	assert.Equal(t, verification.SLSAPredicateV1, approval.ProvenancePredicateType)
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"), approval.SignerWorkflow)
	assert.Equal(t, "/managed/bin", approval.BinDir)
	assert.Equal(t, []string{"foo"}, approval.Binaries)
}

func TestVerifiedInstallerUsesReleaseManifestForAssetBinariesAndSigner(t *testing.T) {
	tc := newInstallTestContext(t)
	evidence := givenSuccessfulInstallFixture(t, tc)
	tc.manifests.data = []byte(maliciousDiscoveryManifest())
	tc.manifests.refData = map[string][]byte{
		"foo-v1.2.3": []byte(testManifest()),
	}

	result, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
	})

	require.NoError(t, err)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", result.AssetName)
	assert.Equal(t, "foo_1.2.3_darwin_arm64.tar.gz", tc.assets.assetName)
	assert.Equal(t, []manifest.Binary{{Path: "bin/foo"}}, tc.archives.request.Binaries)
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"), tc.verifier.request.Policy.TrustedSignerWorkflow)
	assert.Equal(t, evidence.AssetDigest, result.Evidence.AssetDigest)
}

func TestVerifiedInstallerDoesNotMutateWhenApprovalDeclines(t *testing.T) {
	tc := newInstallTestContext(t)
	givenSuccessfulInstallFixture(t, tc)

	_, err := tc.subject.Install(context.Background(), VerifiedInstallRequest{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		PackageName: "foo",
		Version:     "1.2.3",
		StoreDir:    filepath.Join(t.TempDir(), "store-root"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		StateDir:    filepath.Join(t.TempDir(), "state"),
		Platform:    manifest.Platform{OS: "darwin", Arch: "arm64"},
		Approve: func(context.Context, InstallApproval) error {
			tc.events = append(tc.events, "approval")
			return ErrInstallNotApproved
		},
	})

	require.ErrorIs(t, err, ErrInstallNotApproved)
	assert.False(t, tc.files.storeCalled)
	assert.False(t, tc.archives.called)
	assert.Nil(t, tc.evidence.record)
	assert.Nil(t, tc.files.metadata)
	assert.Equal(t, []string{"state-load", "download-dir", "approval", "cleanup"}, tc.events)
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

func TestVerifiedInstallerRejectsBinaryOwnershipCollisionBeforeDownloading(t *testing.T) {
	tc := newInstallTestContext(t)
	var err error
	existing := installedRecord("owner/other", "bar")
	existing.Binaries = []state.Binary{
		{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/bar/extracted/foo"},
	}
	tc.state.index, err = tc.state.index.AddRecord(existing)
	require.NoError(t, err)
	tc.manifests.data = []byte(testManifest())

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
	var conflict state.BinaryOwnershipConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, "foo", conflict.Binary)
	assert.Equal(t, state.PackageRef{Repository: "owner/other", Package: "bar"}, conflict.Owner)
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

func givenSuccessfulInstallFixture(t *testing.T, tc *installTestContext) verification.Evidence {
	t.Helper()

	assetDigest := mustDigest(t, "sha256", repeatHex("aa", 32))
	releaseDigest := mustDigest(t, "sha1", repeatHex("bb", 20))
	evidence := verification.Evidence{
		Repository:       verification.Repository{Owner: "owner", Name: "repo"},
		Tag:              "foo-v1.2.3",
		AssetDigest:      assetDigest,
		ReleaseTagDigest: releaseDigest,
		ReleaseAttestation: verification.AttestationEvidence{
			AttestationID: "release",
			PredicateType: verification.ReleasePredicateV02,
		},
		ProvenanceAttestation: verification.AttestationEvidence{
			AttestationID:  "provenance",
			PredicateType:  verification.SLSAPredicateV1,
			SignerWorkflow: "owner/repo/.github/workflows/release.yml",
		},
	}
	tc.manifests.data = []byte(testManifest())
	tc.assets.asset = ReleaseAsset{Name: "foo_1.2.3_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
	tc.downloader.path = filepath.Join(t.TempDir(), "foo.tar.gz")
	tc.verifier.evidence = evidence
	tc.files.downloadDir = t.TempDir()
	tc.files.layout = StoreLayout{
		StorePath:    filepath.Join(t.TempDir(), "store"),
		ArtifactPath: filepath.Join(t.TempDir(), "store", "artifact"),
		ExtractedDir: filepath.Join(t.TempDir(), "store", "extracted"),
	}
	tc.archives.result = []ExtractedBinary{{Name: "foo", RelativePath: "bin/foo", Path: "/store/extracted/bin/foo"}}
	tc.files.links = []InstalledBinary{{Name: "foo", LinkPath: "/bin/foo", TargetPath: "/store/extracted/bin/foo"}}
	return evidence
}

type eventEvidenceWriter struct {
	events  *[]string
	path    string
	record  *VerificationRecord
	records map[string]VerificationRecord
	err     error
}

func (f *eventEvidenceWriter) WriteVerificationEvidence(_ context.Context, _ string, record VerificationRecord) (string, error) {
	*f.events = append(*f.events, "evidence")
	if f.err != nil {
		return "", f.err
	}
	f.record = &record
	return f.path, nil
}

func (f *eventEvidenceWriter) ReadVerificationRecord(_ context.Context, path string) (VerificationRecord, error) {
	if record, ok := f.records[path]; ok {
		return record, nil
	}
	return VerificationRecord{}, errors.New("verification record not found")
}

func (f *eventEvidenceWriter) StoreInstalledRecords(t *testing.T, records ...state.Record) {
	t.Helper()
	if f.records == nil {
		f.records = map[string]VerificationRecord{}
	}
	for _, record := range records {
		repository, err := parseRecordRepository(record.Repository)
		require.NoError(t, err)
		f.records[record.VerificationPath] = VerificationRecord{
			SchemaVersion: 1,
			Repository:    record.Repository,
			Package:       record.Package,
			Version:       record.Version,
			Tag:           record.Tag,
			Asset:         record.Asset,
			Evidence: verification.Evidence{
				Repository:  repository,
				Tag:         verification.ReleaseTag(record.Tag),
				AssetDigest: mustDigest(t, "sha256", repeatHex("aa", 32)),
				ProvenanceAttestation: verification.AttestationEvidence{
					SignerWorkflow: verification.WorkflowIdentity(record.Repository + "/.github/workflows/release.yml"),
				},
			},
		}
	}
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
	if f.result == nil {
		out := make([]ExtractedBinary, 0, len(request.Binaries))
		for _, binary := range request.Binaries {
			out = append(out, ExtractedBinary{
				Name:         filepath.Base(filepath.FromSlash(binary.Path)),
				RelativePath: binary.Path,
				Path:         filepath.Join(request.DestinationDir, filepath.FromSlash(binary.Path)),
			})
		}
		return out, nil
	}
	return f.result, nil
}

type fakeInstallFileSystem struct {
	events           *[]string
	downloadDir      string
	storeRequest     StoreLayoutRequest
	layout           StoreLayout
	links            []InstalledBinary
	metadata         *InstallRecord
	metadataErr      error
	storeCalled      bool
	linkErr          error
	replaceErr       error
	replaceErrs      []error
	removeStoreErr   error
	removeStoreErrs  []error
	removeManagedErr error
	removedManaged   *RemoveManagedInstallRequest
	replaceRequests  []ReplaceManagedBinariesRequest
	removedStoreRoot string
	removedStorePath string
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
	if f.removeManagedErr != nil {
		return f.removeManagedErr
	}
	copied := request
	copied.Binaries = append([]InstalledBinary(nil), request.Binaries...)
	f.removedManaged = &copied
	return nil
}

func (f *fakeInstallFileSystem) ReplaceManagedBinaries(_ context.Context, request ReplaceManagedBinariesRequest) error {
	*f.events = append(*f.events, "replace-binaries")
	copied := ReplaceManagedBinariesRequest{
		BinDir:   request.BinDir,
		Previous: append([]InstalledBinary(nil), request.Previous...),
		Next:     append([]InstalledBinary(nil), request.Next...),
	}
	f.replaceRequests = append(f.replaceRequests, copied)
	if len(f.replaceErrs) > 0 {
		err := f.replaceErrs[0]
		f.replaceErrs = f.replaceErrs[1:]
		return err
	}
	return f.replaceErr
}

func (f *fakeInstallFileSystem) RemoveManagedStore(_ context.Context, storeRoot string, storePath string) error {
	*f.events = append(*f.events, "remove-store")
	f.removedStoreRoot = storeRoot
	f.removedStorePath = storePath
	if len(f.removeStoreErrs) > 0 {
		err := f.removeStoreErrs[0]
		f.removeStoreErrs = f.removeStoreErrs[1:]
		return err
	}
	return f.removeStoreErr
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
	events         *[]string
	index          state.Index
	saved          state.Index
	replaced       state.Index
	replacedRecord state.Record
	loadErr        error
	saveErr        error
	replaceErr     error
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

func (f *fakeInstalledStateStore) ReplaceInstalledRecord(_ context.Context, _ string, record state.Record) (state.Index, error) {
	if f.events != nil {
		*f.events = append(*f.events, "state-replace")
	}
	f.replacedRecord = record
	if f.replaceErr != nil {
		return state.Index{}, f.replaceErr
	}
	index, err := f.index.ReplaceRecord(record)
	if err != nil {
		return state.Index{}, err
	}
	f.replaced = index.Normalize()
	f.index = f.replaced
	return f.replaced, nil
}
