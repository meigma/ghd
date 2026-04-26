package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/verification"
)

func TestCatalogStoreLoadsMissingIndexAsEmpty(t *testing.T) {
	store := NewCatalogStore()

	index, err := store.LoadCatalog(context.Background(), filepath.Join(t.TempDir(), "index"))

	require.NoError(t, err)
	assert.Equal(t, 1, index.SchemaVersion)
	assert.Empty(t, index.Repositories)
}

func TestCatalogStoreWritesAndLoadsRepositoryIndex(t *testing.T) {
	indexDir := filepath.Join(t.TempDir(), "index")
	record := catalog.RepositoryRecord{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		Packages:    []catalog.PackageSummary{{Name: "foo", Description: "Foo CLI", Binaries: []string{"foo"}}},
		RefreshedAt: time.Unix(1700000000, 0).UTC(),
	}
	store := NewCatalogStore()

	index, err := store.UpsertRepository(context.Background(), indexDir, record)
	require.NoError(t, err)
	loaded, err := store.LoadCatalog(context.Background(), indexDir)

	require.NoError(t, err)
	assert.Equal(t, index.Normalize(), loaded)
	assert.FileExists(t, filepath.Join(indexDir, "repositories.json"))
}

func TestCatalogStoreUpsertRepositoryMergesConcurrentWriters(t *testing.T) {
	store := NewCatalogStore()
	indexDir := filepath.Join(t.TempDir(), "index")
	const repositories = 8
	var wg sync.WaitGroup
	errs := make(chan error, repositories)

	for i := range repositories {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			record := catalog.RepositoryRecord{
				Repository:  verification.Repository{Owner: "owner", Name: fmt.Sprintf("repo-%d", i)},
				Packages:    []catalog.PackageSummary{{Name: "foo", Binaries: []string{"foo"}}},
				RefreshedAt: time.Unix(1700000000, 0).UTC(),
			}
			errs <- func() error {
				_, err := store.UpsertRepository(context.Background(), indexDir, record)
				return err
			}()
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	loaded, err := store.LoadCatalog(context.Background(), indexDir)

	require.NoError(t, err)
	assert.Len(t, loaded.Repositories, repositories)
}

func TestCatalogStoreRemoveRepository(t *testing.T) {
	store := NewCatalogStore()
	indexDir := filepath.Join(t.TempDir(), "index")
	_, err := store.UpsertRepository(context.Background(), indexDir, catalog.RepositoryRecord{
		Repository:  verification.Repository{Owner: "owner", Name: "repo"},
		Packages:    []catalog.PackageSummary{{Name: "foo", Binaries: []string{"foo"}}},
		RefreshedAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)
	_, err = store.UpsertRepository(context.Background(), indexDir, catalog.RepositoryRecord{
		Repository:  verification.Repository{Owner: "owner", Name: "other"},
		Packages:    []catalog.PackageSummary{{Name: "bar", Binaries: []string{"bar"}}},
		RefreshedAt: time.Unix(1700000000, 0).UTC(),
	})
	require.NoError(t, err)

	index, err := store.RemoveRepository(
		context.Background(),
		indexDir,
		verification.Repository{Owner: "owner", Name: "repo"},
	)

	require.NoError(t, err)
	assert.Len(t, index.Repositories, 1)
	assert.Equal(t, "other", index.Repositories[0].Repository.Name)
	loaded, err := store.LoadCatalog(context.Background(), indexDir)
	require.NoError(t, err)
	assert.Equal(t, index, loaded)

	_, err = store.RemoveRepository(
		context.Background(),
		indexDir,
		verification.Repository{Owner: "owner", Name: "missing"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not indexed")
}

func TestCatalogStoreRejectsMalformedJSON(t *testing.T) {
	indexDir := filepath.Join(t.TempDir(), "index")
	require.NoError(t, os.MkdirAll(indexDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(indexDir, "repositories.json"), []byte("{"), 0o644))
	store := NewCatalogStore()

	_, err := store.LoadCatalog(context.Background(), indexDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode catalog index")
}

func TestCatalogStoreRejectsMissingSchemaVersion(t *testing.T) {
	indexDir := filepath.Join(t.TempDir(), "index")
	require.NoError(t, os.MkdirAll(indexDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(indexDir, "repositories.json"), []byte("{}\n"), 0o644))

	_, err := NewCatalogStore().LoadCatalog(context.Background(), indexDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported catalog index version 0")
}
