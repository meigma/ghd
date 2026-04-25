package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

func TestRepositoryCatalogAddFetchesManifestAndPersistsRecord(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.manifests.data["owner/repo"] = []byte(testManifest())

	record, err := tc.subject.AddRepository(context.Background(), RepositoryAddRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
		IndexDir:   filepath.Join(t.TempDir(), "index"),
	})

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, record.Repository)
	assert.Equal(t, []catalog.PackageSummary{{Name: "foo", Binaries: []string{"foo"}}}, record.Packages)
	assert.Len(t, tc.store.saved.Repositories, 1)
}

func TestRepositoryCatalogRemoveOnlyUpdatesLocalIndex(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.store.index = catalog.NewIndex()
	var err error
	tc.store.index, err = tc.store.index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "repo"}, singlePackageManifestConfig("foo", "")))
	require.NoError(t, err)

	err = tc.subject.RemoveRepository(context.Background(), RepositoryRemoveRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
		IndexDir:   filepath.Join(t.TempDir(), "index"),
	})

	require.NoError(t, err)
	assert.Empty(t, tc.store.saved.Repositories)
	assert.Empty(t, tc.manifests.data, "remove should not fetch manifests")
}

func TestRepositoryCatalogRefreshesOneRepository(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.store.index = catalog.NewIndex()
	var err error
	tc.store.index, err = tc.store.index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "repo"}, singlePackageManifestConfig("old", "")))
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = []byte(testManifest())

	result, err := tc.subject.RefreshRepositories(context.Background(), RepositoryRefreshRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
		IndexDir:   filepath.Join(t.TempDir(), "index"),
	})

	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "foo", result[0].Packages[0].Name)
	assert.Equal(t, "foo", tc.store.saved.Repositories[0].Packages[0].Name)
}

func TestRepositoryCatalogResolvesPackagesAndReportsAmbiguity(t *testing.T) {
	index := catalog.NewIndex()
	var err error
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "one"}, singlePackageManifestConfig("foo", "")))
	require.NoError(t, err)
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "two"}, singlePackageManifestConfig("foo", "")))
	require.NoError(t, err)

	tc := newRepositoryCatalogTestContext(t)
	tc.store.index = index

	_, err = tc.subject.ResolvePackage(context.Background(), ResolvePackageRequest{
		PackageName: "foo",
		IndexDir:    filepath.Join(t.TempDir(), "index"),
	})

	require.Error(t, err)
	var ambiguous catalog.AmbiguousPackageError
	require.ErrorAs(t, err, &ambiguous)
}

func TestRepositoryCatalogListPackagesFromLocalIndex(t *testing.T) {
	index := catalog.NewIndex()
	var err error
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "zeta"}, singlePackageManifestConfig("zap", "")))
	require.NoError(t, err)
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "alpha"}, multiPackageManifestConfig()))
	require.NoError(t, err)

	tc := newRepositoryCatalogTestContext(t)
	tc.store.index = index

	results, err := tc.subject.ListPackages(context.Background(), PackageListRequest{
		IndexDir: filepath.Join(t.TempDir(), "index"),
	})

	require.NoError(t, err)
	assert.Equal(t, []PackageListResult{
		{Repository: verification.Repository{Owner: "owner", Name: "alpha"}, PackageName: "bar", Binaries: []string{"bar"}},
		{Repository: verification.Repository{Owner: "owner", Name: "alpha"}, PackageName: "foo", Binaries: []string{"foo"}},
		{Repository: verification.Repository{Owner: "owner", Name: "zeta"}, PackageName: "zap", Binaries: []string{"zap"}},
	}, results)
}

func TestRepositoryCatalogListPackagesLiveRepositoryDoesNotPersist(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.manifests.data["owner/multi"] = mustMarshalManifest(t, multiPackageManifestConfig())

	results, err := tc.subject.ListPackages(context.Background(), PackageListRequest{
		Repository: verification.Repository{Owner: "owner", Name: "multi"},
	})

	require.NoError(t, err)
	assert.Equal(t, []PackageListResult{
		{Repository: verification.Repository{Owner: "owner", Name: "multi"}, PackageName: "bar", Binaries: []string{"bar"}},
		{Repository: verification.Repository{Owner: "owner", Name: "multi"}, PackageName: "foo", Binaries: []string{"foo"}},
	}, results)
	assert.False(t, tc.store.saveCalled)
}

func TestRepositoryCatalogInfoPackageResolvesUnqualifiedName(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	var err error
	tc.store.index, err = tc.store.index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "repo"}, singlePackageManifestConfig("foo", "")))
	require.NoError(t, err)
	tc.manifests.data["owner/repo"] = mustMarshalManifest(t, singlePackageManifestConfig("foo", ""))

	result, err := tc.subject.InfoPackage(context.Background(), PackageInfoRequest{
		UnqualifiedName: "foo",
		IndexDir:        filepath.Join(t.TempDir(), "index"),
	})

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, result.Repository)
	assert.Equal(t, "foo", result.PackageName.String())
	assert.Equal(t, verification.WorkflowIdentity("owner/repo/.github/workflows/release.yml"), result.SignerWorkflow)
	assert.Equal(t, "v${version}", result.TagPattern)
	assert.Equal(t, []string{"foo"}, result.Binaries)
	assert.Equal(t, []PackageInfoAsset{
		{OS: "darwin", Arch: "arm64", Pattern: "foo_${version}_darwin_arm64.tar.gz"},
		{OS: "linux", Arch: "amd64", Pattern: "foo_${version}_linux_amd64.tar.gz"},
	}, result.Assets)
}

func TestRepositoryCatalogInfoPackageReportsAmbiguousUnqualifiedName(t *testing.T) {
	index := catalog.NewIndex()
	var err error
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "one"}, singlePackageManifestConfig("foo", "")))
	require.NoError(t, err)
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "two"}, singlePackageManifestConfig("foo", "")))
	require.NoError(t, err)

	tc := newRepositoryCatalogTestContext(t)
	tc.store.index = index

	_, err = tc.subject.InfoPackage(context.Background(), PackageInfoRequest{
		UnqualifiedName: "foo",
		IndexDir:        filepath.Join(t.TempDir(), "index"),
	})

	require.Error(t, err)
	var ambiguous catalog.AmbiguousPackageError
	require.ErrorAs(t, err, &ambiguous)
}

func TestRepositoryCatalogInfoPackageAutoSelectsSinglePackageRepository(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.manifests.data["owner/repo"] = mustMarshalManifest(t, singlePackageManifestConfig("foo", ""))

	result, err := tc.subject.InfoPackage(context.Background(), PackageInfoRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
	})

	require.NoError(t, err)
	assert.Equal(t, "foo", result.PackageName.String())
	assert.Equal(t, "v${version}", result.TagPattern)
}

func TestRepositoryCatalogInfoPackageRequiresQualificationForMultiPackageRepository(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.manifests.data["owner/multi"] = mustMarshalManifest(t, multiPackageManifestConfig())

	_, err := tc.subject.InfoPackage(context.Background(), PackageInfoRequest{
		Repository: verification.Repository{Owner: "owner", Name: "multi"},
	})

	require.Error(t, err)
	assert.EqualError(t, err, "repository owner/multi declares multiple packages; use owner/multi/package")
}

func TestRepositoryCatalogDoesNotSaveWhenManifestFetchFails(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.manifests.err["owner/repo"] = errors.New("not found")

	_, err := tc.subject.AddRepository(context.Background(), RepositoryAddRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
		IndexDir:   filepath.Join(t.TempDir(), "index"),
	})

	require.Error(t, err)
	assert.False(t, tc.store.saveCalled)
}

type repositoryCatalogTestContext struct {
	manifests *fakeManifestRouter
	store     *fakeCatalogStore
	subject   *RepositoryCatalog
}

func newRepositoryCatalogTestContext(t *testing.T) *repositoryCatalogTestContext {
	t.Helper()
	tc := &repositoryCatalogTestContext{
		manifests: &fakeManifestRouter{
			data: map[string][]byte{},
			err:  map[string]error{},
		},
		store: &fakeCatalogStore{index: catalog.NewIndex()},
	}
	subject, err := NewRepositoryCatalog(RepositoryCatalogDependencies{
		Manifests: tc.manifests,
		Store:     tc.store,
		Now:       func() time.Time { return time.Unix(1700000000, 0) },
	})
	require.NoError(t, err)
	tc.subject = subject
	return tc
}

type fakeCatalogStore struct {
	index      catalog.Index
	saved      catalog.Index
	saveCalled bool
	err        error
}

func (f *fakeCatalogStore) LoadCatalog(context.Context, string) (catalog.Index, error) {
	if f.err != nil {
		return catalog.Index{}, f.err
	}
	return f.index, nil
}

func (f *fakeCatalogStore) UpsertRepository(_ context.Context, _ string, record catalog.RepositoryRecord) (catalog.Index, error) {
	if f.err != nil {
		return catalog.Index{}, f.err
	}
	index, err := f.index.UpsertRepository(record)
	if err != nil {
		return catalog.Index{}, err
	}
	return f.save(index)
}

func (f *fakeCatalogStore) UpsertRepositories(_ context.Context, _ string, records []catalog.RepositoryRecord) (catalog.Index, error) {
	if f.err != nil {
		return catalog.Index{}, f.err
	}
	index := f.index
	var err error
	for _, record := range records {
		index, err = index.UpsertRepository(record)
		if err != nil {
			return catalog.Index{}, err
		}
	}
	return f.save(index)
}

func (f *fakeCatalogStore) RemoveRepository(_ context.Context, _ string, repository verification.Repository) (catalog.Index, error) {
	if f.err != nil {
		return catalog.Index{}, f.err
	}
	index, removed := f.index.RemoveRepository(repository)
	if !removed {
		return catalog.Index{}, errors.New("repository is not indexed")
	}
	return f.save(index)
}

func (f *fakeCatalogStore) save(index catalog.Index) (catalog.Index, error) {
	if f.err != nil {
		return catalog.Index{}, f.err
	}
	f.saveCalled = true
	f.saved = index
	f.index = index
	return index, nil
}

func repositoryRecord(t *testing.T, repository verification.Repository, cfg manifest.Config) catalog.RepositoryRecord {
	t.Helper()
	record, err := catalog.NewRepositoryRecord(repository, cfg, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return record
}

func singlePackageManifestConfig(packageName string, tagPattern string) manifest.Config {
	pkg := manifest.Package{
		Name:        manifest.PackageName(packageName),
		Description: "Test package",
		Assets: []manifest.Asset{
			{OS: "darwin", Arch: "arm64", Pattern: packageName + "_${version}_darwin_arm64.tar.gz"},
			{OS: "linux", Arch: "amd64", Pattern: packageName + "_${version}_linux_amd64.tar.gz"},
		},
		Binaries: []manifest.Binary{{Path: "bin/" + packageName}},
	}
	if tagPattern != "" {
		pkg.TagPattern = tagPattern
	}
	return manifest.Config{
		Version: manifest.SchemaVersion,
		Provenance: manifest.Provenance{
			SignerWorkflow: "owner/repo/.github/workflows/release.yml",
		},
		Packages: []manifest.Package{pkg},
	}
}

func multiPackageManifestConfig() manifest.Config {
	return manifest.Config{
		Version: manifest.SchemaVersion,
		Provenance: manifest.Provenance{
			SignerWorkflow: "owner/multi/.github/workflows/release.yml",
		},
		Packages: []manifest.Package{
			{
				Name:        "foo",
				Description: "Foo CLI",
				Assets: []manifest.Asset{
					{OS: "darwin", Arch: "arm64", Pattern: "foo_${version}_darwin_arm64.tar.gz"},
					{OS: "linux", Arch: "amd64", Pattern: "foo_${version}_linux_amd64.tar.gz"},
				},
				Binaries: []manifest.Binary{{Path: "bin/foo"}},
			},
			{
				Name:        "bar",
				Description: "Bar CLI",
				TagPattern:  "bar-v${version}",
				Assets: []manifest.Asset{
					{OS: "darwin", Arch: "arm64", Pattern: "bar_${version}_darwin_arm64.tar.gz"},
					{OS: "linux", Arch: "amd64", Pattern: "bar_${version}_linux_amd64.tar.gz"},
				},
				Binaries: []manifest.Binary{{Path: "bin/bar"}},
			},
		},
	}
}

func mustMarshalManifest(t *testing.T, cfg manifest.Config) []byte {
	t.Helper()
	data, err := toml.Marshal(cfg)
	require.NoError(t, err)
	return data
}
