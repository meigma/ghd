package filesystem

import (
	"context"
	"os"
	"path/filepath"
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
	index, err := catalog.NewIndex().UpsertRepository(record)
	require.NoError(t, err)
	store := NewCatalogStore()

	require.NoError(t, store.SaveCatalog(context.Background(), indexDir, index))
	loaded, err := store.LoadCatalog(context.Background(), indexDir)

	require.NoError(t, err)
	assert.Equal(t, index.Normalize(), loaded)
	assert.FileExists(t, filepath.Join(indexDir, "repositories.json"))
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
