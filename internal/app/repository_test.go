package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

func TestRepositoryCatalogAddFetchesManifestAndPersistsRecord(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.manifests.data = []byte(testManifest())

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
	tc.store.index, err = tc.store.index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "repo"}, "foo"))
	require.NoError(t, err)

	err = tc.subject.RemoveRepository(context.Background(), RepositoryRemoveRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
		IndexDir:   filepath.Join(t.TempDir(), "index"),
	})

	require.NoError(t, err)
	assert.Empty(t, tc.store.saved.Repositories)
	assert.Nil(t, tc.manifests.data, "remove should not fetch manifests")
}

func TestRepositoryCatalogRefreshesOneRepository(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.store.index = catalog.NewIndex()
	var err error
	tc.store.index, err = tc.store.index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "repo"}, "old"))
	require.NoError(t, err)
	tc.manifests.data = []byte(testManifest())

	result, err := tc.subject.RefreshRepositories(context.Background(), RepositoryRefreshRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
		IndexDir:   filepath.Join(t.TempDir(), "index"),
	})

	require.NoError(t, err)
	require.Len(t, result.Repositories, 1)
	assert.Equal(t, "foo", result.Repositories[0].Packages[0].Name)
	assert.Equal(t, "foo", tc.store.saved.Repositories[0].Packages[0].Name)
}

func TestRepositoryCatalogResolvesPackagesAndReportsAmbiguity(t *testing.T) {
	index := catalog.NewIndex()
	var err error
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "one"}, "foo"))
	require.NoError(t, err)
	index, err = index.UpsertRepository(repositoryRecord(t, verification.Repository{Owner: "owner", Name: "two"}, "foo"))
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

func TestRepositoryCatalogDoesNotSaveWhenManifestFetchFails(t *testing.T) {
	tc := newRepositoryCatalogTestContext(t)
	tc.manifests.err = errors.New("not found")

	_, err := tc.subject.AddRepository(context.Background(), RepositoryAddRequest{
		Repository: verification.Repository{Owner: "owner", Name: "repo"},
		IndexDir:   filepath.Join(t.TempDir(), "index"),
	})

	require.Error(t, err)
	assert.False(t, tc.store.saveCalled)
}

type repositoryCatalogTestContext struct {
	manifests *fakeManifestSource
	store     *fakeCatalogStore
	subject   *RepositoryCatalog
}

func newRepositoryCatalogTestContext(t *testing.T) *repositoryCatalogTestContext {
	t.Helper()
	tc := &repositoryCatalogTestContext{
		manifests: &fakeManifestSource{},
		store:     &fakeCatalogStore{index: catalog.NewIndex()},
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

func (f *fakeCatalogStore) SaveCatalog(_ context.Context, _ string, index catalog.Index) error {
	if f.err != nil {
		return f.err
	}
	f.saveCalled = true
	f.saved = index
	f.index = index
	return nil
}

func repositoryRecord(t *testing.T, repository verification.Repository, packageName string) catalog.RepositoryRecord {
	t.Helper()
	record, err := catalog.NewRepositoryRecord(repository, manifestConfig(packageName), time.Unix(1700000000, 0))
	require.NoError(t, err)
	return record
}

func manifestConfig(packageName string) manifest.Config {
	return manifest.Config{
		Version: manifest.SchemaVersion,
		Provenance: manifest.Provenance{
			SignerWorkflow: "owner/repo/.github/workflows/release.yml",
		},
		Packages: []manifest.Package{
			{Name: packageName, Binaries: []manifest.Binary{{Path: "bin/" + packageName}}},
		},
	}
}
