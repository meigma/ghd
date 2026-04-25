package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/state"
	"github.com/meigma/ghd/internal/verification"
)

func TestInstalledPackageCheckerReportsUpdateForSingleTarget(t *testing.T) {
	tc := newInstalledPackageCheckerTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.2.3", AssetNames: []string{"foo_1.2.3_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.1.0", AssetNames: []string{"foo_1.1.0_darwin_arm64.tar.gz"}},
	}

	results, err := tc.subject.Check(context.Background(), CheckRequest{
		Target:   "foo",
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, CheckStatusUpdateAvailable, results[0].Status)
	assert.Equal(t, "1.3.0", results[0].LatestVersion)
}

func TestInstalledPackageCheckerReportsUpToDateWhenNoNewerStableReleaseExists(t *testing.T) {
	tc := newInstalledPackageCheckerTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.2.3", AssetNames: []string{"foo_1.2.3_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.3.0", Prerelease: true, AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.4.0", Draft: true, AssetNames: []string{"foo_1.4.0_darwin_arm64.tar.gz"}},
	}

	results, err := tc.subject.Check(context.Background(), CheckRequest{
		Target:   "owner/repo/foo",
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, CheckStatusUpToDate, results[0].Status)
	assert.Empty(t, results[0].LatestVersion)
}

func TestInstalledPackageCheckerRejectsAmbiguousTargets(t *testing.T) {
	tc := newInstalledPackageCheckerTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(withInstalledBinaries(installedRecord("owner/one", "foo"), []state.Binary{
		{Name: "one", LinkPath: "/bin/one", TargetPath: "/store/foo/extracted/one"},
	}))
	require.NoError(t, err)
	tc.state.index, err = tc.state.index.AddRecord(withInstalledBinaries(installedRecord("owner/two", "bar"), []state.Binary{
		{Name: "foo", LinkPath: "/bin/foo-two", TargetPath: "/store/bar/extracted/foo"},
	}))
	require.NoError(t, err)

	_, err = tc.subject.Check(context.Background(), CheckRequest{
		Target:   "foo",
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var ambiguous state.AmbiguousInstallError
	require.ErrorAs(t, err, &ambiguous)
}

func TestInstalledPackageCheckerRejectsUnsupportedInstalledVersions(t *testing.T) {
	tc := newInstalledPackageCheckerTestContext(t)
	var err error
	record := installedRecord("owner/repo", "foo")
	record.Version = "rolling"
	tc.state.index, err = tc.state.index.AddRecord(record)
	require.NoError(t, err)

	_, err = tc.subject.Check(context.Background(), CheckRequest{
		Target:   "foo",
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "supported semantic version")
}

func TestInstalledPackageCheckerReturnsManifestFetchFailuresForSingleTarget(t *testing.T) {
	tc := newInstalledPackageCheckerTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/repo", "foo"))
	require.NoError(t, err)
	tc.manifests.err["owner/repo"] = errors.New("missing")

	_, err = tc.subject.Check(context.Background(), CheckRequest{
		Target:   "foo",
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch ghd.toml")
}

func TestInstalledPackageCheckerAggregatesCannotDetermineResultsForAllTargets(t *testing.T) {
	tc := newInstalledPackageCheckerTestContext(t)
	var err error
	tc.state.index, err = tc.state.index.AddRecord(installedRecord("owner/one", "foo"))
	require.NoError(t, err)
	tc.state.index, err = tc.state.index.AddRecord(withInstalledBinaries(installedRecord("owner/two", "bar"), []state.Binary{
		{Name: "bar", LinkPath: "/bin/bar", TargetPath: "/store/bar/extracted/bar"},
	}))
	require.NoError(t, err)
	tc.manifests.data["owner/one"] = []byte(testManifest())
	tc.releases.data["owner/one"] = []RepositoryRelease{{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}}}
	tc.manifests.err["owner/two"] = errors.New("missing")

	results, err := tc.subject.Check(context.Background(), CheckRequest{
		All:      true,
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.Error(t, err)
	var incomplete CheckIncompleteError
	require.ErrorAs(t, err, &incomplete)
	assert.Equal(t, 1, incomplete.Failed)
	require.Len(t, results, 2)
	assert.Equal(t, CheckStatusUpdateAvailable, results[0].Status)
	assert.Equal(t, "1.3.0", results[0].LatestVersion)
	assert.Equal(t, CheckStatusCannotDetermine, results[1].Status)
	assert.Contains(t, results[1].Reason, "fetch ghd.toml")
}

func TestLatestStablePackageReleaseForPlatformChoosesHighestStableRelease(t *testing.T) {
	minimumVersion, err := normalizeSemver("1.2.3")
	require.NoError(t, err)

	repository := verification.Repository{Owner: "owner", Name: "repo"}
	packageName, err := manifest.NewPackageName("foo")
	require.NoError(t, err)
	manifests := &fakeManifestRouter{
		data:    map[string][]byte{},
		refData: map[string][]byte{},
		err:     map[string]error{},
		refErr:  map[string]error{},
	}
	manifests.refData[manifestRefKey(repository.String(), "foo-v1.3.0")] = []byte(testManifest())
	manifests.refData[manifestRefKey(repository.String(), "foo-v1.10.0")] = []byte(testManifest())
	manifests.refData[manifestRefKey(repository.String(), "foo-v1.2.4")] = []byte(testManifest())

	latest, err := latestStablePackageReleaseForPlatform(context.Background(), manifests, repository, packageName, []RepositoryRelease{
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.10.0", AssetNames: []string{"foo_1.10.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.2.4", AssetNames: []string{"foo_1.2.4_darwin_arm64.tar.gz"}},
	}, manifest.Platform{OS: "darwin", Arch: "arm64"}, minimumVersion)

	require.NoError(t, err)
	assert.Equal(t, "1.10.0", latest.Version.String())
	assert.Equal(t, verification.ReleaseTag("foo-v1.10.0"), latest.Tag)
	assert.Equal(t, "foo_1.10.0_darwin_arm64.tar.gz", latest.AssetName)
}

func TestLatestStablePackageReleaseForPlatformIgnoresDraftsPrereleasesAndInvalidTags(t *testing.T) {
	minimumVersion, err := normalizeSemver("1.2.3")
	require.NoError(t, err)

	repository := verification.Repository{Owner: "owner", Name: "repo"}
	packageName, err := manifest.NewPackageName("foo")
	require.NoError(t, err)
	manifests := &fakeManifestRouter{
		data:    map[string][]byte{},
		refData: map[string][]byte{},
		err:     map[string]error{},
		refErr:  map[string]error{},
	}
	manifests.refData[manifestRefKey(repository.String(), "foo-v1.3.0")] = []byte(testManifest())
	manifests.refData[manifestRefKey(repository.String(), "foo-v1.6.0-rc.1")] = []byte(testManifest())

	latest, err := latestStablePackageReleaseForPlatform(context.Background(), manifests, repository, packageName, []RepositoryRelease{
		{TagName: "foo-v1.4.0", Draft: true, AssetNames: []string{"foo_1.4.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.5.0", Prerelease: true, AssetNames: []string{"foo_1.5.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.6.0-rc.1", AssetNames: []string{"foo_1.6.0-rc.1_darwin_arm64.tar.gz"}},
		{TagName: "other-v2.0.0", AssetNames: []string{"foo_2.0.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
	}, manifest.Platform{OS: "darwin", Arch: "arm64"}, minimumVersion)

	require.NoError(t, err)
	assert.Equal(t, "1.3.0", latest.Version.String())
}

func TestLatestStablePackageReleaseForPlatformSkipsMissingManifestAndAssetMismatches(t *testing.T) {
	minimumVersion, err := normalizeSemver("1.2.3")
	require.NoError(t, err)

	repository := verification.Repository{Owner: "owner", Name: "repo"}
	packageName, err := manifest.NewPackageName("foo")
	require.NoError(t, err)
	manifests := &fakeManifestRouter{
		data:    map[string][]byte{},
		refData: map[string][]byte{},
		err:     map[string]error{},
		refErr:  map[string]error{},
	}
	manifests.refErr[manifestRefKey(repository.String(), "foo-v1.4.0")] = errors.New("missing")
	manifests.refData[manifestRefKey(repository.String(), "foo-v1.3.0")] = []byte(testManifest())

	latest, err := latestStablePackageReleaseForPlatform(context.Background(), manifests, repository, packageName, []RepositoryRelease{
		{TagName: "foo-v1.4.0", AssetNames: []string{"foo_1.4.0_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.3.1", AssetNames: []string{"foo_1.3.1_linux_amd64.tar.gz"}},
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
	}, manifest.Platform{OS: "darwin", Arch: "arm64"}, minimumVersion)

	require.NoError(t, err)
	assert.Equal(t, "1.3.0", latest.Version.String())
}

func TestInstalledPackageCheckerAllowsInstalledPrereleasesAndFindsStableUpgrade(t *testing.T) {
	tc := newInstalledPackageCheckerTestContext(t)
	var err error
	record := installedRecord("owner/repo", "foo")
	record.Version = "1.3.0-rc.1"
	record.Tag = "foo-v1.3.0-rc.1"
	record.Asset = "foo_1.3.0-rc.1_darwin_arm64.tar.gz"
	tc.state.index, err = tc.state.index.AddRecord(record)
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(testManifest())
	tc.releases.data["owner/repo"] = []RepositoryRelease{
		{TagName: "foo-v1.3.0-rc.1", Prerelease: true, AssetNames: []string{"foo_1.3.0-rc.1_darwin_arm64.tar.gz"}},
		{TagName: "foo-v1.3.0", AssetNames: []string{"foo_1.3.0_darwin_arm64.tar.gz"}},
	}

	results, err := tc.subject.Check(context.Background(), CheckRequest{
		Target:   "foo",
		StateDir: filepath.Join(t.TempDir(), "state"),
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, CheckStatusUpdateAvailable, results[0].Status)
	assert.Equal(t, "1.3.0", results[0].LatestVersion)
}

type installedPackageCheckerTestContext struct {
	manifests *fakeManifestRouter
	releases  *fakeRepositoryReleaseSource
	state     *fakeInstalledStateStore
	subject   *InstalledPackageChecker
}

func newInstalledPackageCheckerTestContext(t *testing.T) *installedPackageCheckerTestContext {
	t.Helper()
	tc := &installedPackageCheckerTestContext{
		manifests: &fakeManifestRouter{
			data: map[string][]byte{},
			err:  map[string]error{},
		},
		releases: &fakeRepositoryReleaseSource{
			data: map[string][]RepositoryRelease{},
			err:  map[string]error{},
		},
		state: &fakeInstalledStateStore{index: state.NewIndex()},
	}
	subject, err := NewInstalledPackageChecker(InstalledPackageCheckerDependencies{
		Manifests:  tc.manifests,
		Releases:   tc.releases,
		StateStore: tc.state,
	})
	require.NoError(t, err)
	tc.subject = subject
	return tc
}

type fakeManifestRouter struct {
	data    map[string][]byte
	refData map[string][]byte
	err     map[string]error
	refErr  map[string]error
}

func (f *fakeManifestRouter) FetchManifest(_ context.Context, repository verification.Repository) ([]byte, error) {
	if err, ok := f.err[repository.String()]; ok {
		return nil, err
	}
	if data, ok := f.data[repository.String()]; ok {
		return data, nil
	}
	return nil, errors.New("manifest not found")
}

func (f *fakeManifestRouter) FetchManifestAtRef(_ context.Context, repository verification.Repository, ref string) ([]byte, error) {
	key := manifestRefKey(repository.String(), ref)
	if err, ok := f.refErr[key]; ok {
		return nil, err
	}
	if data, ok := f.refData[key]; ok {
		return data, nil
	}
	return f.FetchManifest(context.Background(), repository)
}

func manifestRefKey(repository string, ref string) string {
	return repository + "@" + ref
}

type fakeRepositoryReleaseSource struct {
	data     map[string][]RepositoryRelease
	err      map[string]error
	requests []verification.Repository
}

func (f *fakeRepositoryReleaseSource) ListRepositoryReleases(_ context.Context, repository verification.Repository) ([]RepositoryRelease, error) {
	f.requests = append(f.requests, repository)
	if err, ok := f.err[repository.String()]; ok {
		return nil, err
	}
	return append([]RepositoryRelease(nil), f.data[repository.String()]...), nil
}
