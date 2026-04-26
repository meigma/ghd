package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/catalog"
	"github.com/meigma/ghd/internal/verification"
)

const catalogIndexFile = "repositories.json"
const catalogIndexLockFile = ".repositories.lock"

// CatalogStore persists the local repository catalog as JSON.
type CatalogStore struct{}

// NewCatalogStore creates a filesystem catalog store.
func NewCatalogStore() CatalogStore {
	return CatalogStore{}
}

// LoadCatalog reads the catalog index from indexDir.
func (CatalogStore) LoadCatalog(ctx context.Context, indexDir string) (catalog.Index, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Index{}, err
	}
	if strings.TrimSpace(indexDir) == "" {
		return catalog.Index{}, errors.New("index directory must be set")
	}
	return loadCatalogFile(indexDir)
}

// UpsertRepository adds or replaces an indexed repository under an index lock.
func (CatalogStore) UpsertRepository(
	ctx context.Context,
	indexDir string,
	record catalog.RepositoryRecord,
) (catalog.Index, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Index{}, err
	}
	if strings.TrimSpace(indexDir) == "" {
		return catalog.Index{}, errors.New("index directory must be set")
	}
	unlock, err := acquireCatalogLock(ctx, indexDir)
	if err != nil {
		return catalog.Index{}, err
	}
	defer unlock()

	index, err := loadCatalogFile(indexDir)
	if err != nil {
		return catalog.Index{}, err
	}
	index, err = index.UpsertRepository(record)
	if err != nil {
		return catalog.Index{}, err
	}
	if err := saveCatalogFile(indexDir, index); err != nil {
		return catalog.Index{}, err
	}
	return index.Normalize(), nil
}

// UpsertRepositories adds or replaces indexed repositories under one index lock.
func (CatalogStore) UpsertRepositories(
	ctx context.Context,
	indexDir string,
	records []catalog.RepositoryRecord,
) (catalog.Index, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Index{}, err
	}
	if strings.TrimSpace(indexDir) == "" {
		return catalog.Index{}, errors.New("index directory must be set")
	}
	unlock, err := acquireCatalogLock(ctx, indexDir)
	if err != nil {
		return catalog.Index{}, err
	}
	defer unlock()

	index, err := loadCatalogFile(indexDir)
	if err != nil {
		return catalog.Index{}, err
	}
	for _, record := range records {
		index, err = index.UpsertRepository(record)
		if err != nil {
			return catalog.Index{}, err
		}
	}
	if err := saveCatalogFile(indexDir, index); err != nil {
		return catalog.Index{}, err
	}
	return index.Normalize(), nil
}

// RemoveRepository removes an indexed repository under an index lock.
func (CatalogStore) RemoveRepository(
	ctx context.Context,
	indexDir string,
	repository verification.Repository,
) (catalog.Index, error) {
	if err := ctx.Err(); err != nil {
		return catalog.Index{}, err
	}
	if strings.TrimSpace(indexDir) == "" {
		return catalog.Index{}, errors.New("index directory must be set")
	}
	unlock, err := acquireCatalogLock(ctx, indexDir)
	if err != nil {
		return catalog.Index{}, err
	}
	defer unlock()

	index, err := loadCatalogFile(indexDir)
	if err != nil {
		return catalog.Index{}, err
	}
	index, removed := index.RemoveRepository(repository)
	if !removed {
		return catalog.Index{}, fmt.Errorf("repository %s is not indexed", repository)
	}
	if err := saveCatalogFile(indexDir, index); err != nil {
		return catalog.Index{}, err
	}
	return index.Normalize(), nil
}

func loadCatalogFile(indexDir string) (catalog.Index, error) {
	data, err := os.ReadFile(filepath.Join(indexDir, catalogIndexFile))
	if os.IsNotExist(err) {
		return catalog.NewIndex(), nil
	}
	if err != nil {
		return catalog.Index{}, fmt.Errorf("read catalog index: %w", err)
	}
	var index catalog.Index
	//nolint:musttag // The persisted catalog schema intentionally preserves nested exported field names.
	if err := json.Unmarshal(data, &index); err != nil {
		return catalog.Index{}, fmt.Errorf("decode catalog index: %w", err)
	}
	if err := index.Validate(); err != nil {
		return catalog.Index{}, err
	}
	return index.Normalize(), nil
}

func saveCatalogFile(indexDir string, index catalog.Index) error {
	index = index.Normalize()
	if err := index.Validate(); err != nil {
		return err
	}
	//nolint:musttag // The persisted catalog schema intentionally preserves nested exported field names.
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode catalog index: %w", err)
	}
	data = append(data, '\n')
	_, err = writeFileAtomic(indexDir, catalogIndexFile, data, metadataMode)
	return err
}

func acquireCatalogLock(ctx context.Context, indexDir string) (func(), error) {
	return acquireFileLock(ctx, indexDir, catalogIndexLockFile, "index", "catalog index")
}
