package catalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/manifest"
	"github.com/meigma/ghd/internal/verification"
)

func TestIndexUpsertsAndRemovesRepositories(t *testing.T) {
	repository := verification.Repository{Owner: "owner", Name: "repo"}
	first := newRecord(t, repository, "foo", "Foo CLI")
	replacement := newRecord(t, repository, "bar", "Bar CLI")

	index, err := NewIndex().UpsertRepository(first)
	require.NoError(t, err)
	index, err = index.UpsertRepository(replacement)
	require.NoError(t, err)

	require.Len(t, index.Repositories, 1)
	assert.Equal(t, "bar", index.Repositories[0].Packages[0].Name)

	index, removed := index.RemoveRepository(verification.Repository{Owner: "OWNER", Name: "REPO"})
	assert.True(t, removed)
	assert.Empty(t, index.Repositories)
}

func TestIndexResolvesUnqualifiedPackages(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.UpsertRepository(newRecord(t, verification.Repository{Owner: "owner", Name: "repo"}, "foo", "Foo CLI"))
	require.NoError(t, err)

	resolved, err := index.ResolvePackage("foo")

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, resolved.Repository)
	assert.Equal(t, "foo", resolved.PackageName.String())
}

func TestIndexResolvesUnqualifiedBinaryNames(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.UpsertRepository(newRecordWithBinaries(t, verification.Repository{Owner: "owner", Name: "repo"}, "bar", "Bar CLI", []manifest.Binary{{Path: "bin/foo"}}))
	require.NoError(t, err)

	resolved, err := index.ResolvePackage("foo")

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, resolved.Repository)
	assert.Equal(t, "bar", resolved.PackageName.String())
}

func TestIndexReportsAmbiguousPackages(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.UpsertRepository(newRecord(t, verification.Repository{Owner: "owner", Name: "one"}, "foo", "Foo CLI"))
	require.NoError(t, err)
	index, err = index.UpsertRepository(newRecord(t, verification.Repository{Owner: "owner", Name: "two"}, "foo", "Other Foo CLI"))
	require.NoError(t, err)

	_, err = index.ResolvePackage("foo")

	require.Error(t, err)
	var ambiguous AmbiguousPackageError
	require.ErrorAs(t, err, &ambiguous)
	assert.Equal(t, []ResolvedPackage{
		{Repository: verification.Repository{Owner: "owner", Name: "one"}, PackageName: "foo"},
		{Repository: verification.Repository{Owner: "owner", Name: "two"}, PackageName: "foo"},
	}, ambiguous.Matches)
	assert.Contains(t, err.Error(), "owner/one/foo")
	assert.Contains(t, err.Error(), "owner/two/foo")
}

func TestIndexPrefersExactPackageNameOverBinaryNames(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.UpsertRepository(newRecord(t, verification.Repository{Owner: "owner", Name: "one"}, "foo", "Foo CLI"))
	require.NoError(t, err)
	index, err = index.UpsertRepository(newRecordWithBinaries(t, verification.Repository{Owner: "owner", Name: "two"}, "bar", "Bar CLI", []manifest.Binary{{Path: "bin/foo"}}))
	require.NoError(t, err)

	resolved, err := index.ResolvePackage("foo")

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "one"}, resolved.Repository)
	assert.Equal(t, "foo", resolved.PackageName.String())
}

func TestIndexReportsAmbiguousBinaryNamesWithoutExactPackageMatch(t *testing.T) {
	index := NewIndex()
	var err error
	index, err = index.UpsertRepository(newRecordWithBinaries(t, verification.Repository{Owner: "owner", Name: "one"}, "bar", "Bar CLI", []manifest.Binary{{Path: "bin/foo"}}))
	require.NoError(t, err)
	index, err = index.UpsertRepository(newRecordWithBinaries(t, verification.Repository{Owner: "owner", Name: "two"}, "baz", "Baz CLI", []manifest.Binary{{Path: "bin/foo"}}))
	require.NoError(t, err)

	_, err = index.ResolvePackage("foo")

	require.Error(t, err)
	var ambiguous AmbiguousPackageError
	require.ErrorAs(t, err, &ambiguous)
	assert.Equal(t, []ResolvedPackage{
		{Repository: verification.Repository{Owner: "owner", Name: "one"}, PackageName: "bar"},
		{Repository: verification.Repository{Owner: "owner", Name: "two"}, PackageName: "baz"},
	}, ambiguous.Matches)
	assert.Contains(t, err.Error(), "owner/one/bar")
	assert.Contains(t, err.Error(), "owner/two/baz")
}

func TestNewRepositoryRecordSummarizesManifestPackages(t *testing.T) {
	record, err := NewRepositoryRecord(verification.Repository{Owner: "owner", Name: "repo"}, manifest.Config{
		Version: manifest.SchemaVersion,
		Provenance: manifest.Provenance{
			SignerWorkflow: "owner/repo/.github/workflows/release.yml",
		},
		Packages: []manifest.Package{
			{Name: "zeta", Description: "Zeta CLI", Binaries: []manifest.Binary{{Path: "cmd/zeta"}}},
			{Name: "alpha", Description: " Alpha CLI ", Binaries: []manifest.Binary{{Path: "bin/alpha"}}},
		},
	}, time.Unix(1700000000, 0))

	require.NoError(t, err)
	assert.Equal(t, verification.Repository{Owner: "owner", Name: "repo"}, record.Repository)
	assert.Equal(t, []PackageSummary{
		{Name: "alpha", Description: "Alpha CLI", Binaries: []string{"alpha"}},
		{Name: "zeta", Description: "Zeta CLI", Binaries: []string{"zeta"}},
	}, record.Packages)
	assert.Equal(t, time.Unix(1700000000, 0).UTC(), record.RefreshedAt)
}

func newRecord(t *testing.T, repository verification.Repository, name string, description string) RepositoryRecord {
	t.Helper()
	return newRecordWithBinaries(t, repository, name, description, []manifest.Binary{{Path: "bin/" + name}})
}

func newRecordWithBinaries(t *testing.T, repository verification.Repository, name string, description string, binaries []manifest.Binary) RepositoryRecord {
	t.Helper()
	record, err := NewRepositoryRecord(repository, manifest.Config{
		Version: manifest.SchemaVersion,
		Provenance: manifest.Provenance{
			SignerWorkflow: repository.String() + "/.github/workflows/release.yml",
		},
		Packages: []manifest.Package{
			{Name: manifest.PackageName(name), Description: description, Binaries: binaries},
		},
	}, time.Unix(1700000000, 0))
	require.NoError(t, err)
	return record
}
