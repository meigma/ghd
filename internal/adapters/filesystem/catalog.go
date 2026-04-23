package filesystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/ghd/internal/catalog"
)

const catalogIndexFile = "repositories.json"

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
		return catalog.Index{}, fmt.Errorf("index directory must be set")
	}
	data, err := os.ReadFile(filepath.Join(indexDir, catalogIndexFile))
	if os.IsNotExist(err) {
		return catalog.NewIndex(), nil
	}
	if err != nil {
		return catalog.Index{}, fmt.Errorf("read catalog index: %w", err)
	}
	var index catalog.Index
	if err := json.Unmarshal(data, &index); err != nil {
		return catalog.Index{}, fmt.Errorf("decode catalog index: %w", err)
	}
	index = index.Normalize()
	if err := index.Validate(); err != nil {
		return catalog.Index{}, err
	}
	return index, nil
}

// SaveCatalog writes the catalog index to indexDir.
func (CatalogStore) SaveCatalog(ctx context.Context, indexDir string, index catalog.Index) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(indexDir) == "" {
		return fmt.Errorf("index directory must be set")
	}
	index = index.Normalize()
	if err := index.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode catalog index: %w", err)
	}
	data = append(data, '\n')
	_, err = writeFileAtomic(indexDir, catalogIndexFile, data, 0o644)
	return err
}
