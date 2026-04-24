package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

func TestPackageUpdaterUpdateSingleTargetUpdatedRowAfterSuccessfulStaging(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	record := updateInstalledRecord("owner/repo", "1.2.3")
	tc.state.index, err = mustUpdateIndex(record)
	require.NoError(t, err)
	configureRepositoryForVersion(t, tc, record.Repository, "1.3.0")
	configureSuccessfulUpdateFixture(t, tc, "1.3.0")
	storeDir := filepath.Join(t.TempDir(), "store-root")
	binDir := filepath.Join(t.TempDir(), "bin")

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: storeDir,
		BinDir:   binDir,
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, UpdateInstalledResult{
		Repository:      "owner/repo",
		Package:         "foo",
		PreviousVersion: "1.2.3",
		CurrentVersion:  "1.3.0",
		Status:          UpdateStatusUpdated,
	}, results[0])
	require.NotNil(t, tc.files.metadata)
	assert.Equal(t, "1.3.0", tc.files.metadata.Version)
	assert.Equal(t, "foo_1.3.0_darwin_arm64.tar.gz", tc.files.metadata.Asset)
	require.Len(t, tc.files.replaceRequests, 1)
	assert.Equal(t, record.Binaries[0].LinkPath, tc.files.replaceRequests[0].Previous[0].LinkPath)
	expectedLinkPath, err := filepath.Abs(filepath.Join(binDir, "foo"))
	require.NoError(t, err)
	assert.Equal(t, expectedLinkPath, tc.files.replaceRequests[0].Next[0].LinkPath)
	assert.Equal(t, "1.3.0", tc.state.replacedRecord.Version)
	assert.Equal(t, tc.files.layout.StorePath, tc.state.replacedRecord.StorePath)
	assert.Equal(t, storeDir, tc.files.removedStoreRoot)
	assert.Equal(t, record.StorePath, tc.files.removedStorePath)
	assert.Nil(t, tc.downloader.request.Progress)
	assert.Equal(t, []string{"state-load", "state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "remove-store", "cleanup"}, tc.events)
}

func TestPackageUpdaterRejectsBinaryOwnershipCollisionBeforeDownloading(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	record := updateInstalledRecord("owner/repo", "1.2.3")
	conflicting := updateInstalledRecordForPackage("owner/other", "bar", "1.2.3")
	conflicting.Binaries = []state.Binary{{Name: "bar", LinkPath: "/bin/bar", TargetPath: "/store/other/bar/extracted/bar"}}
	var err error
	tc.state.index, err = mustUpdateIndex(record, conflicting)
	require.NoError(t, err)
	tc.manifests.data[record.Repository] = []byte(testManifestWithBinary("bin/bar"))
	tc.releases.data[record.Repository] = []RepositoryRelease{{
		TagName:    "foo-v1.3.0",
		AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"},
	}}

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "owner/repo/foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var incomplete UpdateIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Failed)
	require.Len(t, results, 1)
	assert.Equal(t, UpdateStatusCannotUpdate, results[0].Status)
	assert.Contains(t, results[0].Reason, `binary "bar" is already owned by owner/other/bar`)
	assert.False(t, tc.files.storeCalled)
	assert.False(t, tc.archives.called)
	assert.Empty(t, tc.files.replaceRequests)
	assert.Equal(t, []string{"state-load", "state-load"}, tc.events)
}

func TestPackageUpdaterUpdateSingleTargetAlreadyUpToDate(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	record := updateInstalledRecord("owner/repo", "1.3.0")
	tc.state.index, err = mustUpdateIndex(record)
	require.NoError(t, err)
	configureRepositoryForVersion(t, tc, record.Repository, "1.3.0")

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, UpdateInstalledResult{
		Repository:      "owner/repo",
		Package:         "foo",
		PreviousVersion: "1.3.0",
		CurrentVersion:  "1.3.0",
		Status:          UpdateStatusAlreadyUpToDate,
	}, results[0])
	assert.Empty(t, tc.files.replaceRequests)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load"}, tc.events)
}

func TestPackageUpdaterUpdateSingleTargetCannotUpdateWhenInstalledAssetVariantDrifts(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	record := updateInstalledRecord("owner/repo", "1.2.3")
	tc.state.index, err = mustUpdateIndex(record)
	require.NoError(t, err)
	tc.manifests.data[record.Repository] = []byte(`
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
	tc.releases.data[record.Repository] = []RepositoryRelease{
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_linux_amd64.tar.gz"}},
	}

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var incomplete UpdateIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Failed)
	assert.Equal(t, "could not update 1 installed package", err.Error())
	require.Len(t, results, 1)
	assert.Equal(t, UpdateStatusCannotUpdate, results[0].Status)
	assert.Contains(t, results[0].Reason, "installed asset")
	assert.Empty(t, tc.files.replaceRequests)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load"}, tc.events)
}

func TestPackageUpdaterUpdateSingleTargetCannotUpdateAndRollsBackLinksWhenStateReplacementFails(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	record := updateInstalledRecord("owner/repo", "1.2.3")
	tc.state.index, err = mustUpdateIndex(record)
	require.NoError(t, err)
	tc.state.replaceErr = errors.New("write installed state")
	configureRepositoryForVersion(t, tc, record.Repository, "1.3.0")
	configureSuccessfulUpdateFixture(t, tc, "1.3.0")

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var incomplete UpdateIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Failed)
	require.Len(t, results, 1)
	assert.Equal(t, UpdateStatusCannotUpdate, results[0].Status)
	assert.Contains(t, results[0].Reason, "replace installed state")
	require.Len(t, tc.files.replaceRequests, 2)
	assert.Equal(t, record.Binaries[0].LinkPath, tc.files.replaceRequests[0].Previous[0].LinkPath)
	assert.Equal(t, record.Binaries[0].LinkPath, tc.files.replaceRequests[1].Next[0].LinkPath)
	require.NotNil(t, tc.files.removedManaged)
	assert.Equal(t, tc.files.layout.StorePath, tc.files.removedManaged.StorePath)
	assert.Empty(t, tc.files.removedManaged.Binaries)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load", "state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "replace-binaries", "remove-managed", "cleanup"}, tc.events)
}

func TestPackageUpdaterUpdateSingleTargetPreservesStagedStoreWhenRollbackFails(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	record := updateInstalledRecord("owner/repo", "1.2.3")
	tc.state.index, err = mustUpdateIndex(record)
	require.NoError(t, err)
	tc.state.replaceErr = errors.New("write installed state")
	tc.files.replaceErrs = []error{nil, errors.New("restore links")}
	configureRepositoryForVersion(t, tc, record.Repository, "1.3.0")
	configureSuccessfulUpdateFixture(t, tc, "1.3.0")

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var incomplete UpdateIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Failed)
	require.Len(t, results, 1)
	assert.Equal(t, UpdateStatusCannotUpdate, results[0].Status)
	assert.Contains(t, results[0].Reason, "preserved staged update")
	require.Len(t, tc.files.replaceRequests, 2)
	assert.Nil(t, tc.files.removedManaged)
	assert.Empty(t, tc.files.removedStorePath)
	assert.Equal(t, []string{"state-load", "state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "replace-binaries", "cleanup"}, tc.events)
}

func TestPackageUpdaterUpdateSingleTargetUpdatedWithWarningWhenPreviousStoreCleanupFails(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	var err error
	record := updateInstalledRecord("owner/repo", "1.2.3")
	tc.state.index, err = mustUpdateIndex(record)
	require.NoError(t, err)
	tc.files.removeStoreErr = errors.New("permission denied")
	configureRepositoryForVersion(t, tc, record.Repository, "1.3.0")
	configureSuccessfulUpdateFixture(t, tc, "1.3.0")

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var incomplete UpdateIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Warned)
	assert.Equal(t, "updated 1 installed package with warnings", err.Error())
	require.Len(t, results, 1)
	assert.Equal(t, UpdateInstalledResult{
		Repository:      "owner/repo",
		Package:         "foo",
		PreviousVersion: "1.2.3",
		CurrentVersion:  "1.3.0",
		Status:          UpdateStatusUpdatedWithWarning,
		Reason:          `updated owner/repo/foo@1.2.3 -> 1.3.0 but failed to remove previous store: permission denied`,
	}, results[0])
	assert.Equal(t, "1.3.0", tc.state.replacedRecord.Version)
	assert.Equal(t, []string{"state-load", "state-load", "download-dir", "store-layout", "extract", "evidence", "metadata", "replace-binaries", "state-replace", "remove-store", "cleanup"}, tc.events)
}

func TestPackageUpdaterUpdateAllSuccess(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	alpha := updateInstalledRecordForPackage("owner/alpha", "alpha", "1.2.3")
	repo := updateInstalledRecord("owner/repo", "1.2.3")
	var err error
	tc.state.index, err = mustUpdateIndex(alpha, repo)
	require.NoError(t, err)
	configureRepositoryRecordForVersion(t, tc, alpha, "1.3.0")
	configureRepositoryForVersion(t, tc, repo.Repository, "1.3.0")
	configureSuccessfulUpdateFixture(t, tc, "1.3.0")

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		All:      true,
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, UpdateInstalledResult{
		Repository:      "owner/alpha",
		Package:         "alpha",
		PreviousVersion: "1.2.3",
		CurrentVersion:  "1.3.0",
		Status:          UpdateStatusUpdated,
	}, results[0])
	assert.Equal(t, UpdateInstalledResult{
		Repository:      "owner/repo",
		Package:         "foo",
		PreviousVersion: "1.2.3",
		CurrentVersion:  "1.3.0",
		Status:          UpdateStatusUpdated,
	}, results[1])
}

func TestPackageUpdaterUpdateAllMixedWarningAndFailure(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)
	broken := updateInstalledRecordForPackage("owner/broken", "broken", "1.2.3")
	current := updateInstalledRecordForPackage("owner/current", "current", "1.3.0")
	repo := updateInstalledRecord("owner/repo", "1.2.3")
	warn := updateInstalledRecordForPackage("owner/warn", "warn", "1.2.3")
	var err error
	tc.state.index, err = mustUpdateIndex(broken, current, repo, warn)
	require.NoError(t, err)
	tc.manifests.err[broken.Repository] = errors.New("missing")
	configureRepositoryRecordForVersion(t, tc, current, "1.3.0")
	configureRepositoryForVersion(t, tc, repo.Repository, "1.3.0")
	configureRepositoryRecordForVersion(t, tc, warn, "1.3.0")
	configureSuccessfulUpdateFixture(t, tc, "1.3.0")
	tc.files.removeStoreErrs = []error{nil, errors.New("permission denied")}

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		All:      true,
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var incomplete UpdateIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Failed)
	assert.Equal(t, 1, incomplete.Warned)
	assert.Equal(t, "update completed with 1 warning and 1 failure", err.Error())
	require.Len(t, results, 4)
	assert.Equal(t, UpdateStatusCannotUpdate, results[0].Status)
	assert.Contains(t, results[0].Reason, "fetch ghd.toml: missing")
	assert.Equal(t, UpdateInstalledResult{
		Repository:      "owner/current",
		Package:         "current",
		PreviousVersion: "1.3.0",
		CurrentVersion:  "1.3.0",
		Status:          UpdateStatusAlreadyUpToDate,
	}, results[1])
	assert.Equal(t, UpdateStatusUpdated, results[2].Status)
	assert.Equal(t, UpdateStatusUpdatedWithWarning, results[3].Status)
	assert.Contains(t, results[3].Reason, "failed to remove previous store")
}

func TestPackageUpdaterUpdateRejectsInvalidRequests(t *testing.T) {
	tc := newPackageUpdaterTestContext(t)

	results, err := tc.subject.Update(context.Background(), UpdateRequest{
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Nil(t, results)
	assert.EqualError(t, err, "update target must be set")

	results, err = tc.subject.Update(context.Background(), UpdateRequest{
		Target:   "foo",
		All:      true,
		StoreDir: filepath.Join(t.TempDir(), "store-root"),
		BinDir:   filepath.Join(t.TempDir(), "bin"),
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Nil(t, results)
	assert.EqualError(t, err, "update accepts a target or --all, not both")
}

func TestPackageUpdaterUpdateReturnsPreflightErrorsWithoutResults(t *testing.T) {
	t.Run("state load failure", func(t *testing.T) {
		tc := newPackageUpdaterTestContext(t)
		tc.state.loadErr = errors.New("boom")

		results, err := tc.subject.Update(context.Background(), UpdateRequest{
			Target:   "foo",
			StoreDir: filepath.Join(t.TempDir(), "store-root"),
			BinDir:   filepath.Join(t.TempDir(), "bin"),
			StateDir: filepath.Join(t.TempDir(), "state"),
		})

		require.Error(t, err)
		assert.Nil(t, results)
		assert.EqualError(t, err, "boom")
	})

	t.Run("ambiguous target", func(t *testing.T) {
		tc := newPackageUpdaterTestContext(t)
		one := updateInstalledRecord("owner/one", "1.2.3")
		one.Binaries = []state.Binary{{Name: "one", LinkPath: "/bin/one", TargetPath: "/store/one/foo/extracted/one"}}
		two := updateInstalledRecordForPackage("owner/two", "bar", "1.2.3")
		two.Binaries = []state.Binary{{Name: "foo", LinkPath: "/bin/foo-two", TargetPath: "/store/two/bar/extracted/foo"}}
		var err error
		tc.state.index, err = mustUpdateIndex(one, two)
		require.NoError(t, err)

		results, err := tc.subject.Update(context.Background(), UpdateRequest{
			Target:   "foo",
			StoreDir: filepath.Join(t.TempDir(), "store-root"),
			BinDir:   filepath.Join(t.TempDir(), "bin"),
			StateDir: filepath.Join(t.TempDir(), "state"),
		})

		require.Error(t, err)
		assert.Nil(t, results)
		var ambiguous state.AmbiguousInstallError
		require.ErrorAs(t, err, &ambiguous)
	})
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

func configureSuccessfulUpdateFixture(t *testing.T, tc *packageUpdaterTestContext, latestVersion string) {
	t.Helper()
	tc.assets.asset = ReleaseAsset{Name: "foo_" + latestVersion + "_darwin_arm64.tar.gz", DownloadURL: "https://example.test/foo.tar.gz"}
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
}

func configureRepositoryForVersion(t *testing.T, tc *packageUpdaterTestContext, repository string, latestVersion string) {
	t.Helper()
	tc.manifests.data[repository] = []byte(testManifest())
	tc.releases.data[repository] = []RepositoryRelease{{
		TagName:    "foo-v" + latestVersion,
		AssetNames: []string{"foo_" + latestVersion + "_darwin_arm64.tar.gz"},
	}}
}

func configureRepositoryRecordForVersion(t *testing.T, tc *packageUpdaterTestContext, record state.Record, latestVersion string) {
	t.Helper()
	tc.manifests.data[record.Repository] = []byte(testManifestForPackage(record.Package))
	tc.releases.data[record.Repository] = []RepositoryRelease{{
		TagName:    record.Package + "-v" + latestVersion,
		AssetNames: []string{record.Package + "_" + latestVersion + "_darwin_arm64.tar.gz"},
	}}
}

func updateInstalledRecord(repository string, version string) state.Record {
	return updateInstalledRecordForPackage(repository, "foo", version)
}

func updateInstalledRecordForPackage(repository string, packageName string, version string) state.Record {
	slug := strings.ReplaceAll(repository, "/", "-")
	return state.Record{
		Repository:       repository,
		Package:          packageName,
		Version:          version,
		Tag:              packageName + "-v" + version,
		Asset:            packageName + "_" + version + "_darwin_arm64.tar.gz",
		AssetDigest:      "sha256:abc123",
		StorePath:        filepath.Join("/store", slug, packageName),
		ArtifactPath:     filepath.Join("/store", slug, packageName, "artifact"),
		ExtractedPath:    filepath.Join("/store", slug, packageName, "extracted"),
		VerificationPath: filepath.Join("/store", slug, packageName, "verification.json"),
		Binaries:         []state.Binary{{Name: packageName, LinkPath: filepath.Join("/bin", packageName), TargetPath: filepath.Join("/store", slug, packageName, "extracted", packageName)}},
		InstalledAt:      time.Unix(1700000000, 0).UTC(),
	}
}

func testManifestWithBinary(binaryPath string) string {
	return strings.Replace(testManifest(), "path = \"bin/foo\"", "path = \""+binaryPath+"\"", 1)
}

func testManifestForPackage(packageName string) string {
	return strings.ReplaceAll(strings.ReplaceAll(testManifest(), "foo", packageName), "owner/repo", "owner/"+packageName)
}

func mustUpdateIndex(records ...state.Record) (state.Index, error) {
	index := state.NewIndex()
	var err error
	for _, record := range records {
		index, err = index.AddRecord(record)
		if err != nil {
			return state.Index{}, err
		}
	}
	return index.Normalize(), nil
}
